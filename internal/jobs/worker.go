package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"webvirtcloud-go/internal/command"
	"webvirtcloud-go/internal/config"
	"webvirtcloud-go/internal/id"
	"webvirtcloud-go/internal/secret"
	"webvirtcloud-go/internal/store"
)

const (
	jobHeartbeatInterval     = 15 * time.Second
	jobHeartbeatStoreTimeout = 5 * time.Second
	jobHeartbeatFailureLimit = 3
)

// Worker consumes DB-backed jobs. PostgreSQL's SKIP LOCKED gives safe
// horizontal scaling without adding Redis or another queue dependency.
type Worker struct {
	cfg    config.Config
	store  *store.Store
	cipher *secret.Cipher
	runner *command.Runner
	log    *slog.Logger
	id     string
}

func New(cfg config.Config, db *store.Store, cipher *secret.Cipher, runner *command.Runner, log *slog.Logger) *Worker {
	return &Worker{cfg: cfg, store: db, cipher: cipher, runner: runner, log: log, id: id.New("worker")}
}

func (w *Worker) Run(ctx context.Context) {
	w.publishHeartbeat(ctx)
	if removed, err := w.store.RecoverStaleJobs(ctx, 5*time.Minute); err != nil {
		w.log.Error("recover stale jobs", "error", err)
	} else if removed > 0 {
		w.log.Warn("marked stale jobs failed", "count", removed)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if removed, err := w.store.RecoverStaleJobs(ctx, 5*time.Minute); err != nil {
					w.log.Error("recover stale jobs", "error", err)
				} else if removed > 0 {
					w.log.Warn("marked stale jobs failed", "count", removed)
				}
			}
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.publishHeartbeat(ctx)
				if _, err := w.store.PurgeWorkerHeartbeats(ctx, 10*time.Minute); err != nil && ctx.Err() == nil {
					w.log.Warn("purge worker heartbeats", "error", err)
				}
			}
		}
	}()
	for i := 0; i < w.cfg.Workers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			w.loop(ctx, fmt.Sprintf("%s-%d", w.id, n))
		}(i)
	}
	wg.Wait()
}

func (w *Worker) publishHeartbeat(ctx context.Context) {
	hbCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := w.store.UpsertWorkerHeartbeat(hbCtx, w.id); err != nil && ctx.Err() == nil {
		w.log.Warn("publish worker heartbeat", "error", err)
	}
}

func (w *Worker) loop(ctx context.Context, workerID string) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for {
				job, err := w.store.ClaimJob(ctx, workerID)
				if err != nil {
					if ctx.Err() == nil {
						w.log.Error("claim job", "error", err)
					}
					break
				}
				if job == nil {
					break
				}
				w.executeSafely(ctx, workerID, job)
			}
		}
	}
}

func (w *Worker) executeSafely(parent context.Context, workerID string, job *store.Job) {
	defer func() {
		if recovered := recover(); recovered != nil {
			w.log.Error("job execution panicked outside command runner",
				"job_id", job.ID, "worker_id", workerID,
				"panic", fmt.Sprint(recovered), "stack", string(debug.Stack()))
			w.finalizePanickedJob(workerID, job)
		}
	}()
	w.execute(parent, workerID, job)
}

func (w *Worker) finalizePanickedJob(workerID string, job *store.Job) {
	defer func() {
		if recovered := recover(); recovered != nil {
			w.log.Error("failed to finalize panicked job",
				"job_id", job.ID, "worker_id", workerID,
				"panic", fmt.Sprint(recovered), "stack", string(debug.Stack()))
		}
	}()
	w.finalize(workerID, job, "failed", "", "任务执行器发生内部错误（panic），详情见服务端日志")
}

func (w *Worker) execute(parent context.Context, workerID string, job *store.Job) {
	runCtx, cancel := context.WithCancel(parent)

	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		defer func() {
			if recovered := recover(); recovered != nil {
				w.log.Error("job monitor panicked",
					"job_id", job.ID, "worker_id", workerID,
					"panic", fmt.Sprint(recovered), "stack", string(debug.Stack()))
				cancel()
			}
		}()
		cancelTicker := time.NewTicker(time.Second)
		heartbeatTicker := time.NewTicker(jobHeartbeatInterval)
		defer cancelTicker.Stop()
		defer heartbeatTicker.Stop()
		heartbeatFailures := 0
		for {
			select {
			case <-runCtx.Done():
				return
			case <-cancelTicker.C:
				checkCtx, checkCancel := context.WithTimeout(runCtx, jobHeartbeatStoreTimeout)
				requested, err := w.store.JobCancelRequested(checkCtx, job.ID)
				checkCancel()
				if err == nil && requested {
					cancel()
					return
				}
			case <-heartbeatTicker.C:
				heartbeatCtx, heartbeatCancel := context.WithTimeout(runCtx, jobHeartbeatStoreTimeout)
				active, err := w.store.HeartbeatJob(heartbeatCtx, job.ID, workerID)
				heartbeatCancel()
				if err != nil && runCtx.Err() == nil {
					heartbeatFailures++
					w.log.Warn("job heartbeat failed", "job_id", job.ID,
						"consecutive_failures", heartbeatFailures, "error", err)
					if heartbeatFailures >= jobHeartbeatFailureLimit {
						// The job queue may transfer ownership after its stale
						// timeout. Stop the command before that happens rather than
						// continuing a host mutation that the database can no longer
						// attribute to this worker.
						w.log.Error("job heartbeat unavailable; stopping execution", "job_id", job.ID,
							"worker_id", workerID, "consecutive_failures", heartbeatFailures)
						cancel()
						return
					}
				} else if !active && runCtx.Err() == nil {
					w.log.Warn("job ownership lost; stopping execution", "job_id", job.ID, "worker_id", workerID)
					cancel()
					return
				} else if err == nil {
					heartbeatFailures = 0
				}
			}
		}
	}()

	var status, output, errText string
	func() {
		// Always stop and join the monitor before finalizing the job. This also
		// runs when the runner panics, so executeSafely cannot leave a monitor
		// goroutine behind while it records the panic.
		defer func() {
			cancel()
			<-monitorDone
		}()
		status, output, errText = w.runJobSafely(runCtx, job)
	}()

	w.finalize(workerID, job, status, output, errText)
}

func (w *Worker) finalize(workerID string, job *store.Job, status, output, errText string) {

	finishCtx, finishCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer finishCancel()
	finished, err := w.store.FinishJob(finishCtx, job.ID, workerID, status, output, errText)
	if err != nil {
		w.log.Error("finish job", "job_id", job.ID, "error", err)
		return
	}
	if !finished {
		w.log.Warn("job state was already finalized; preserving the existing result", "job_id", job.ID, "worker_id", workerID)
		return
	}

	label := job.Label
	if job.Sensitive {
		label = job.Tool + " [敏感参数已隐藏]"
	}
	detail := map[string]any{
		"job_id":   job.ID,
		"label":    label,
		"tool":     job.Tool,
		"status":   status,
		"duration": time.Since(job.CreatedAt).Round(time.Millisecond).String(),
	}
	if errText != "" && !job.Sensitive {
		detail["error"] = errText
	}
	actorName := job.UserID
	if user, err := w.store.GetUserByID(finishCtx, job.UserID); err == nil {
		actorName = user.Username
	}
	if err := w.store.AddAuditReliable(finishCtx, store.Audit{
		ActorID:   job.UserID,
		ActorName: actorName,
		Action:    "job.finished",
		HostID:    job.HostID,
		Detail:    detail,
	}); err != nil {
		w.log.Error("write job completion audit", "job_id", job.ID, "error", err)
	}
}

func (w *Worker) runJobSafely(ctx context.Context, job *store.Job) (status, output, errText string) {
	defer w.recoverJobPanic(job, &status, &output, &errText)
	return w.runJob(ctx, job)
}

func (w *Worker) recoverJobPanic(job *store.Job, status, output, errText *string) {
	recovered := recover()
	if recovered == nil {
		return
	}
	*status = "failed"
	*output = ""
	*errText = "任务执行器发生内部错误（panic），详情见服务端日志"
	w.log.Error("job runner panicked", "job_id", job.ID, "tool", job.Tool,
		"panic", fmt.Sprint(recovered), "stack", string(debug.Stack()))
}

func (w *Worker) runJob(ctx context.Context, job *store.Job) (string, string, string) {
	var args []string
	if err := json.Unmarshal([]byte(job.ArgsJSON), &args); err != nil {
		w.log.Error("decode job arguments", "job_id", job.ID, "tool", job.Tool, "error", err)
		return "failed", "", "保存的任务参数损坏，详情见服务端日志"
	}
	readOnly, _ := command.Classify(job.Tool, args)
	if job.ReadOnly && !readOnly {
		return "failed", "", "任务被标记为只读，但命令不是只读操作，已拒绝执行"
	}
	sensitive := job.Sensitive
	if job.Kind == "command" {
		computed, _ := command.Sensitive(job.Tool, args)
		sensitive = sensitive || computed
	}
	job.Sensitive = sensitive
	user, err := w.store.GetUserByID(ctx, job.UserID)
	if err != nil {
		if !store.IsNotFound(err) {
			w.log.Error("load job user", "job_id", job.ID, "user_id", job.UserID, "error", err)
		}
		return "failed", "", "任务发起人不存在或无法读取"
	}
	if err := validateJobPrincipal(user, job.Kind, job.Tool, args, sensitive); err != nil {
		return "failed", "", err.Error()
	}
	if strings.TrimSpace(job.HostID) == "" {
		return "failed", "", "任务缺少主机配置；为避免误在本机执行，已拒绝运行"
	}

	host, err := w.store.GetHost(ctx, job.HostID)
	if err != nil {
		if store.IsNotFound(err) {
			return "failed", "", "主机配置不存在"
		}
		w.log.Error("load job host", "job_id", job.ID, "host_id", job.HostID, "error", err)
		return "failed", "", "读取主机配置失败，详情见服务端日志"
	}
	if !host.Enabled {
		return "failed", "", "主机已禁用"
	}
	if w.cipher == nil {
		return "failed", "", "服务端加密密钥未配置"
	}
	uri, err := w.cipher.OpenFor("host:"+host.ID, host.URICiphertext)
	if err != nil {
		w.log.Error("decrypt job host credentials", "job_id", job.ID, "host_id", host.ID, "error", err)
		return "failed", "", "解密主机连接信息失败，详情见服务端日志"
	}
	target, err := command.ResolveTarget(strings.TrimSpace(uri))
	if err != nil {
		w.log.Error("resolve job host target", "job_id", job.ID, "host_id", host.ID, "error", err)
		return "failed", "", "解析主机执行目标失败，详情见服务端日志"
	}
	if err := target.Supports(job.Tool); err != nil {
		return "failed", "", err.Error()
	}

	timeout := time.Duration(job.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = w.cfg.CommandTimeout
	}
	req := target.Request(job.Tool, args, job.Stdin, job.ReadOnly, timeout)
	if err := w.runner.Validate(req); err != nil {
		w.log.Warn("validate queued job", "job_id", job.ID, "tool", job.Tool, "error", err)
		return "failed", "", "任务执行前校验失败: " + err.Error()
	}
	start := time.Now()
	result, err := w.runner.Run(ctx, req)
	output := result.Output()

	if ctx.Err() == context.Canceled {
		return "cancelled", output, "任务已取消"
	}
	if err != nil {
		message := err.Error()
		if ctx.Err() == context.DeadlineExceeded {
			message = "命令执行超时"
		}
		return "failed", output, message
	}
	w.log.Info("job succeeded", "job_id", job.ID, "tool", job.Tool, "target", target.Label, "duration", time.Since(start).String())
	return "succeeded", output, ""
}

func validateJobPrincipal(user store.User, kind, tool string, args []string, sensitive bool) error {
	// Bearer-token automation principals are intentionally marked disabled so
	// they cannot authenticate through the browser. Their jobs still need a
	// durable foreign-key owner, but the role is revalidated below on every run.
	if user.Disabled && !store.IsSystemUserID(user.ID) {
		return fmt.Errorf("任务发起人已被禁用")
	}
	if err := authorizeQueuedJob(kind, tool, args, user.Role); err != nil {
		return err
	}
	if sensitive && user.Role != command.RoleAdmin {
		return fmt.Errorf("敏感命令仅允许由当前仍处于启用状态的管理员执行")
	}
	return nil
}

func authorizeQueuedJob(kind, tool string, args []string, role string) error {
	minimumRole, err := command.JobMinimumRole(kind, tool, args)
	if err != nil {
		return fmt.Errorf("任务类型与工具组合不合法: %w", err)
	}
	if !command.RoleAllows(role, minimumRole) {
		return fmt.Errorf("任务发起人的当前角色已无权执行该类任务")
	}
	return nil
}
