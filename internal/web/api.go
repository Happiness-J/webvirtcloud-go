package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"webvirtcloud-go/internal/command"
	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

const apiJSONBodyLimit = 64 * 1024

type apiVMSummary struct {
	Name          string `json:"name"`
	ID            string `json:"id,omitempty"`
	State         string `json:"state"`
	Autostart     string `json:"autostart"`
	VCPUs         int    `json:"vcpus"`
	MaxMemoryKiB  uint64 `json:"max_memory_kib"`
	UsedMemoryKiB uint64 `json:"used_memory_kib,omitempty"`
}

func apiVMSummaryFromDomain(domain libvirt.Domain) apiVMSummary {
	return apiVMSummary{
		Name: domain.Name, ID: domain.ID, State: domain.State, Autostart: domain.Autostart,
		VCPUs: domain.VCPUs, MaxMemoryKiB: domain.MaxMemoryKiB, UsedMemoryKiB: domain.UsedMemoryKiB,
	}
}

type apiGuestAddress struct {
	Interface string `json:"interface"`
	Protocol  string `json:"protocol"`
	Address   string `json:"address"`
	Prefix    string `json:"prefix"`
	Source    string `json:"source"`
}

func apiGuestAddresses(addresses []libvirt.GuestAddress) []apiGuestAddress {
	out := make([]apiGuestAddress, 0, len(addresses))
	for _, address := range addresses {
		out = append(out, apiGuestAddress{
			Interface: address.Interface, Protocol: address.Protocol, Address: address.Address,
			Prefix: address.Prefix, Source: address.Source,
		})
	}
	return out
}

type apiErrorResponse struct {
	Error     string `json:"error"`
	Code      string `json:"code"`
	RequestID string `json:"request_id,omitempty"`
}

func writeAPIError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	writeJSON(w, status, apiErrorResponse{
		Error: message, Code: code, RequestID: requestIDFrom(r.Context()),
	})
}

func decodeAPIJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeAPIError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, apiJSONBodyLimit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeAPIError(w, r, http.StatusRequestEntityTooLarge, "payload_too_large", "Request body exceeds 64 KiB")
			return false
		}
		writeAPIError(w, r, http.StatusBadRequest, "invalid_json", "Request body must be one valid JSON object")
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeAPIError(w, r, http.StatusBadRequest, "invalid_json", "Request body must contain exactly one JSON object")
		return false
	}
	return true
}

type apiVMCreateRequest struct {
	Name      string `json:"name"`
	VCPUs     int    `json:"vcpus"`
	MemoryGB  int    `json:"memory_gb"`
	DiskGB    int    `json:"disk_gb"`
	Network   string `json:"network,omitempty"`
	ISO       string `json:"iso,omitempty"`
	OSVariant string `json:"os_variant,omitempty"`
}

func (s *Server) apiVMCreate(w http.ResponseWriter, r *http.Request) {
	var request apiVMCreateRequest
	if !decodeAPIJSON(w, r, &request) {
		return
	}
	host, err := s.db.GetHost(r.Context(), r.PathValue("id"))
	if err != nil {
		if store.IsNotFound(err) {
			writeAPIError(w, r, http.StatusNotFound, "host_not_found", "Host not found")
		} else {
			s.logRequestError(r, "load API VM create host", err)
			writeAPIError(w, r, http.StatusInternalServerError, "internal_error", "Could not load the host")
		}
		return
	}
	args, err := s.vmCreateArgs(r.Context(), host.ID, request.Name, request.VCPUs, request.MemoryGB, request.DiskGB, request.Network, request.ISO, request.OSVariant, cloudInitSpec{})
	if err != nil {
		writeAPIError(w, r, http.StatusUnprocessableEntity, "invalid_vm_spec", err.Error())
		return
	}
	sess, _ := sessionFrom(r.Context())
	jobID, err := s.enqueueCommand(r, sess.User, host.ID, "vm-create", "API 创建虚拟机 "+strings.TrimSpace(request.Name), "virt-install", args, "", boundedOperationTimeout(s.cfg.MaxTimeout, 1800))
	if err != nil {
		s.logRequestError(r, "enqueue API VM create", err)
		writeAPIError(w, r, http.StatusConflict, "job_rejected", "The host or job queue rejected this operation; use request_id when checking server logs")
		return
	}
	writeJobAccepted(w, jobID)
}

type apiVMActionRequest struct {
	Action  string `json:"action"`
	Confirm bool   `json:"confirm,omitempty"`
}

var apiLifecycleActions = map[string]bool{
	"start": true, "shutdown": true, "reboot": true, "destroy": true,
	"suspend": true, "resume": true, "autostart-on": true, "autostart-off": true,
}

func (s *Server) apiVMAction(w http.ResponseWriter, r *http.Request) {
	var request apiVMActionRequest
	if !decodeAPIJSON(w, r, &request) {
		return
	}
	request.Action = strings.TrimSpace(request.Action)
	if !apiLifecycleActions[request.Action] {
		writeAPIError(w, r, http.StatusBadRequest, "unsupported_action", "Action must be one of start, shutdown, reboot, destroy, suspend, resume, autostart-on or autostart-off")
		return
	}
	host, err := s.db.GetHost(r.Context(), r.PathValue("id"))
	if err != nil {
		if store.IsNotFound(err) {
			writeAPIError(w, r, http.StatusNotFound, "host_not_found", "Host not found")
		} else {
			s.logRequestError(r, "load API VM action host", err)
			writeAPIError(w, r, http.StatusInternalServerError, "internal_error", "Could not load the host")
		}
		return
	}
	vmName := r.PathValue("vm")
	if err := libvirt.ValidateName(vmName); err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "invalid_vm_name", "Virtual machine name is invalid")
		return
	}
	info, err := s.libvirt.DomainInfo(r.Context(), host.ID, vmName)
	if err != nil {
		if errors.Is(err, libvirt.ErrDomainNotFound) {
			writeAPIError(w, r, http.StatusNotFound, "vm_not_found", "Virtual machine not found")
			return
		}
		s.logRequestError(r, "read API VM state", err)
		writeAPIError(w, r, http.StatusBadGateway, "libvirt_unavailable", "Could not read the virtual machine state")
		return
	}
	sess, _ := sessionFrom(r.Context())
	var selected *vmActionView
	for _, action := range vmActions(sess.User.Role, info["State"], info["Autostart"]) {
		if action.Name == request.Action {
			copy := action
			selected = &copy
			break
		}
	}
	if selected == nil {
		writeAPIError(w, r, http.StatusConflict, "action_not_allowed", fmt.Sprintf("Action %s is not allowed while the VM is %s", request.Action, info["State"]))
		return
	}
	if selected.Destructive && !request.Confirm {
		writeAPIError(w, r, http.StatusBadRequest, "confirmation_required", "Set confirm=true for this destructive action")
		return
	}
	args, destructive, err := actionArgs(request.Action, vmName, "")
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "invalid_action", err.Error())
		return
	}
	if destructive && !request.Confirm {
		writeAPIError(w, r, http.StatusBadRequest, "confirmation_required", "Set confirm=true for this destructive action")
		return
	}
	minimumRole, err := command.JobMinimumRole("vm-action", "virsh", args)
	if err != nil || !command.RoleAllows(sess.User.Role, minimumRole) {
		writeAPIError(w, r, http.StatusForbidden, "insufficient_scope", "The API token cannot run this action")
		return
	}
	jobID, err := s.enqueueCommand(r, sess.User, host.ID, "vm-action", "API "+vmName+": "+request.Action, "virsh", args, "", boundedOperationTimeout(s.cfg.MaxTimeout, 300))
	if err != nil {
		s.logRequestError(r, "enqueue API VM action", err)
		writeAPIError(w, r, http.StatusConflict, "job_rejected", "The host or job queue rejected this operation; use request_id when checking server logs")
		return
	}
	writeJobAccepted(w, jobID)
}

type apiVMDeleteRequest struct {
	Confirm       bool   `json:"confirm"`
	ConfirmName   string `json:"confirm_name"`
	DeleteStorage bool   `json:"delete_storage,omitempty"`
}

func (s *Server) apiVMDelete(w http.ResponseWriter, r *http.Request) {
	var request apiVMDeleteRequest
	if !decodeAPIJSON(w, r, &request) {
		return
	}
	host, err := s.db.GetHost(r.Context(), r.PathValue("id"))
	if err != nil {
		if store.IsNotFound(err) {
			writeAPIError(w, r, http.StatusNotFound, "host_not_found", "Host not found")
		} else {
			s.logRequestError(r, "load API VM delete host", err)
			writeAPIError(w, r, http.StatusInternalServerError, "internal_error", "Could not load the host")
		}
		return
	}
	vmName := r.PathValue("vm")
	if err := libvirt.ValidateName(vmName); err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "invalid_vm_name", "Virtual machine name is invalid")
		return
	}
	if !request.Confirm || request.ConfirmName != vmName {
		writeAPIError(w, r, http.StatusBadRequest, "confirmation_required", "Set confirm=true and confirm_name to the exact VM name")
		return
	}
	info, err := s.libvirt.DomainInfo(r.Context(), host.ID, vmName)
	if err != nil {
		if errors.Is(err, libvirt.ErrDomainNotFound) {
			writeAPIError(w, r, http.StatusNotFound, "vm_not_found", "Virtual machine not found")
			return
		}
		s.logRequestError(r, "read API VM state before delete", err)
		writeAPIError(w, r, http.StatusBadGateway, "libvirt_unavailable", "Could not read the virtual machine state")
		return
	}
	state := strings.ToLower(strings.TrimSpace(info["State"]))
	if state != "shut off" && state != "crashed" {
		writeAPIError(w, r, http.StatusConflict, "vm_must_be_stopped", "Shut down the VM before deleting its libvirt definition")
		return
	}
	args := []string{"undefine", vmName, "--snapshots-metadata", "--checkpoints-metadata", "--nvram"}
	if request.DeleteStorage {
		args = append(args, "--remove-all-storage")
	}
	minimumRole, err := command.JobMinimumRole("vm-action", "virsh", args)
	if err != nil || !command.RoleAllows("admin", minimumRole) {
		writeAPIError(w, r, http.StatusInternalServerError, "policy_error", "Delete policy is unavailable")
		return
	}
	sess, _ := sessionFrom(r.Context())
	jobID, err := s.enqueueCommand(r, sess.User, host.ID, "vm-action", "API 删除虚拟机 "+vmName, "virsh", args, "", boundedOperationTimeout(s.cfg.MaxTimeout, 600))
	if err != nil {
		s.logRequestError(r, "enqueue API VM delete", err)
		writeAPIError(w, r, http.StatusConflict, "job_rejected", "The host or job queue rejected this operation; use request_id when checking server logs")
		return
	}
	writeJobAccepted(w, jobID)
}

func writeJobAccepted(w http.ResponseWriter, jobID string) {
	location := "/api/v1/jobs/" + jobID
	w.Header().Set("Location", location)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"job_id": jobID, "status": "queued", "location": location,
	})
}

func (s *Server) vmCreateArgs(ctx context.Context, hostID, name string, vcpus, memoryGB, diskGB int, network, iso, osVariant string, cloud cloudInitSpec) ([]string, error) {
	name = strings.TrimSpace(name)
	if err := libvirt.ValidateName(name); err != nil {
		return nil, errors.New("虚拟机名称无效")
	}
	if vcpus < 1 || vcpus > 128 || memoryGB < 1 || memoryGB > 1024 || diskGB < 1 || diskGB > 10000 {
		return nil, errors.New("CPU 为 1-128，内存为 1-1024 GB，磁盘为 1-10000 GB")
	}
	network = strings.TrimSpace(network)
	if network != "" {
		if err := libvirt.ValidateName(network); err != nil {
			return nil, errors.New("虚拟网络名称无效")
		}
		networks, err := s.libvirt.ListNetworks(ctx, hostID)
		if err != nil {
			return nil, errors.New("无法读取主机虚拟网络")
		}
		allowedNetwork := false
		for _, item := range networks {
			if item.Name == network && item.State == "active" {
				allowedNetwork = true
				break
			}
		}
		if !allowedNetwork {
			return nil, errors.New("只能选择当前主机上已启动的虚拟网络，或留空创建无网络虚拟机")
		}
	}
	iso = strings.TrimSpace(iso)
	if iso != "" && !validInstallISO(s.cfg.ImageDir, iso) {
		return nil, errors.New("ISO 必须是镜像目录中的 .iso 文件")
	}
	osVariant = strings.TrimSpace(osVariant)
	if osVariant == "" {
		osVariant = "generic"
	}
	if len(osVariant) > 128 || strings.IndexFunc(osVariant, func(r rune) bool {
		return !(r == '.' || r == '_' || r == '-' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z')
	}) >= 0 {
		return nil, errors.New("os_variant 只能包含字母、数字、点、下划线和连字符")
	}
	args := []string{
		"--name", name, "--vcpus", strconv.Itoa(vcpus), "--memory", strconv.Itoa(memoryGB * 1024),
		"--disk", "size=" + strconv.Itoa(diskGB) + ",format=qcow2",
		"--osinfo", osVariant, "--graphics", "vnc,listen=127.0.0.1", "--noautoconsole",
	}
	if iso != "" {
		args = append(args, "--cdrom", iso)
	} else {
		// A newly created blank disk needs --import; otherwise virt-install
		// rejects the request because it cannot discover an OS installer.
		args = append(args, "--import")
	}
	if network != "" {
		args = append(args, "--network", "network="+network)
	}
	if cloud.enabled() {
		seed, err := writeCloudInitSeed(s.cfg.ImageDir, cloud)
		if err != nil {
			return nil, err
		}
		args = append(args, "--cloud-init", "user-data="+seed+",network-config="+seed)
	}
	return args, nil
}
