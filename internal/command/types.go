package command

import (
	"errors"
	"strings"
	"time"

	"webvirtcloud-go/internal/remote"
)

var (
	ErrUnknownTool = errors.New("unknown command tool")
	ErrToolMissing = errors.New("command tool is not installed")
	ErrInvalidArgs = errors.New("invalid command arguments")
	ErrNotAllowed  = errors.New("command is not allowed")
)

type Tool struct {
	Name      string
	Path      string
	Available bool
	Required  bool
	Purpose   string
}

type CatalogItem struct {
	Tool              string          `json:"tool"`
	ToolAvailable     bool            `json:"toolAvailable"`
	Command           string          `json:"command"`
	Category          string          `json:"category"`
	Summary           string          `json:"summary"`
	Usage             string          `json:"usage"`
	ReadOnly          bool            `json:"readOnly"`
	Risk              string          `json:"risk"`
	DocsURL           string          `json:"docsUrl,omitempty"`
	Sensitive         bool            `json:"sensitive,omitempty"`
	Disabled          bool            `json:"disabled,omitempty"`
	DisabledReason    string          `json:"disabledReason,omitempty"`
	UnavailableReason string          `json:"unavailableReason,omitempty"`
	ArgPrefix         []string        `json:"argPrefix,omitempty"`
	BuildMode         string          `json:"buildMode,omitempty"`
	Common            []ParameterSpec `json:"common,omitempty"`
	Options           []ParameterSpec `json:"options,omitempty"`
	Positionals       []ParameterSpec `json:"positionals,omitempty"`
	// ProbeState is unknown when discovery was unavailable, advertised when
	// the installed tool listed the command, and missing when it did not.
	ProbeState string `json:"probeState,omitempty"`
}

// ParameterSpec is a form field generated from official command syntax. Flag
// is empty for positional parameters. The server still validates the final
// argv; this structure only drives the browser-side builder.
type ParameterSpec struct {
	Name           string   `json:"name"`
	Flag           string   `json:"flag,omitempty"`
	Label          string   `json:"label"`
	Placeholder    string   `json:"placeholder,omitempty"`
	ValuePrefix    string   `json:"valuePrefix,omitempty"`
	Literal        string   `json:"literal,omitempty"`
	ValueType      string   `json:"valueType"`
	Choices        []string `json:"choices,omitempty"`
	Required       bool     `json:"required,omitempty"`
	Multiple       bool     `json:"multiple,omitempty"`
	OptionalValue  bool     `json:"optionalValue,omitempty"`
	Disabled       bool     `json:"disabled,omitempty"`
	DisabledReason string   `json:"disabledReason,omitempty"`
}

type ToolCapability struct {
	Name      string          `json:"name"`
	Available bool            `json:"available"`
	Version   string          `json:"version,omitempty"`
	Commands  map[string]bool `json:"commands,omitempty"`
	Error     string          `json:"error,omitempty"`
	Note      string          `json:"note,omitempty"`
}

type Discovery struct {
	Tools map[string]ToolCapability `json:"tools"`
}

type Request struct {
	Tool       string
	Args       []string
	Stdin      string
	ConnectURI string
	// SSH changes execution from the panel host to the selected qemu+ssh
	// physical machine. ConnectURI is then the target's local qemu:/// URI for
	// libvirt-aware tools, never a nested qemu+ssh URI.
	SSH      *remote.Target
	ReadOnly bool
	Timeout  time.Duration
}

type Result struct {
	Stdout    string
	Stderr    string
	ExitCode  int
	Truncated bool
	Duration  time.Duration
	Command   []string
}

func (r Result) Output() string {
	var output string
	if r.Stderr == "" {
		output = r.Stdout
	} else if r.Stdout == "" {
		output = r.Stderr
	} else {
		output = r.Stdout + "\n" + r.Stderr
	}
	if r.Truncated {
		if output != "" && !strings.HasSuffix(output, "\n") {
			output += "\n"
		}
		output += "[输出已截断；请缩小命令输出范围或调整 WVC_MAX_OUTPUT_BYTES]"
	}
	return output
}
