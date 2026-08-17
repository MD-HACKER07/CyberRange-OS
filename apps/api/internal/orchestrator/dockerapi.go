package orchestrator

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// dockerAPI is a minimal client for the Docker Engine HTTP API. It supports a
// unix socket, a Windows named pipe, or a TCP(+TLS) endpoint, selected by the
// DOCKER_HOST scheme. Only the endpoints the orchestrator needs are wrapped —
// this keeps the platform free of the very large official Docker SDK while
// still speaking the real Engine API contract.
type dockerAPI struct {
	client  *http.Client
	baseURL string // http://docker or http://host:port
	version string
}

func newDockerAPI(host, version string) (*dockerAPI, error) {
	if version == "" {
		version = "v1.43"
	}
	transport := &http.Transport{}
	baseURL := "http://docker"

	switch {
	case strings.HasPrefix(host, "unix://"):
		sock := strings.TrimPrefix(host, "unix://")
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			d := net.Dialer{}
			return d.DialContext(ctx, "unix", sock)
		}
	case strings.HasPrefix(host, "npipe://"):
		pipe := strings.TrimPrefix(host, "npipe://")
		pipe = "\\\\." + strings.ReplaceAll(strings.TrimPrefix(pipe, "//./"), "/", "\\")
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialNamedPipe(ctx, pipe)
		}
	case strings.HasPrefix(host, "tcp://"), strings.HasPrefix(host, "http://"), strings.HasPrefix(host, "https://"):
		u, err := url.Parse(strings.Replace(host, "tcp://", "http://", 1))
		if err != nil {
			return nil, err
		}
		baseURL = u.Scheme + "://" + u.Host
	default:
		return nil, fmt.Errorf("unsupported DOCKER_HOST scheme: %q", host)
	}

	return &dockerAPI{
		client:  &http.Client{Transport: transport, Timeout: 5 * time.Minute},
		baseURL: baseURL,
		version: version,
	}, nil
}

func (d *dockerAPI) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, d.baseURL+"/"+d.version+path, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return d.client.Do(req)
}

func (d *dockerAPI) doJSON(ctx context.Context, method, path string, body, out any) error {
	res, err := d.do(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return fmt.Errorf("docker api %s %s -> %d: %s", method, path, res.StatusCode, strings.TrimSpace(string(msg)))
	}
	if out != nil {
		return json.NewDecoder(res.Body).Decode(out)
	}
	return nil
}

func (d *dockerAPI) ping(ctx context.Context) error {
	res, err := d.do(ctx, http.MethodGet, "/_ping", nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("docker ping returned %d", res.StatusCode)
	}
	return nil
}

// ------------------------------------------------------------------ networks

type networkCreateRequest struct {
	Name           string            `json:"Name"`
	Driver         string            `json:"Driver"`
	Internal       bool              `json:"Internal"` // Internal=true => no gateway to WAN
	CheckDuplicate bool              `json:"CheckDuplicate"`
	Labels         map[string]string `json:"Labels"`
	IPAM           *ipam             `json:"IPAM,omitempty"`
}

type ipam struct {
	Config []ipamConfig `json:"Config"`
}

type ipamConfig struct {
	Subnet string `json:"Subnet"`
}

func (d *dockerAPI) createNetwork(ctx context.Context, req networkCreateRequest) (string, error) {
	var out struct {
		ID string `json:"Id"`
	}
	if err := d.doJSON(ctx, http.MethodPost, "/networks/create", req, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

func (d *dockerAPI) removeNetwork(ctx context.Context, id string) error {
	res, err := d.do(ctx, http.MethodDelete, "/networks/"+id, nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 && res.StatusCode != http.StatusNotFound {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return fmt.Errorf("remove network %d: %s", res.StatusCode, string(msg))
	}
	return nil
}

// ---------------------------------------------------------------- containers

type containerCreateRequest struct {
	Image        string              `json:"Image"`
	Hostname     string              `json:"Hostname"`
	Cmd          []string            `json:"Cmd,omitempty"`
	Tty          bool                `json:"Tty,omitempty"`
	Labels       map[string]string   `json:"Labels"`
	Env          []string            `json:"Env,omitempty"`
	ExposedPorts map[string]struct{} `json:"ExposedPorts,omitempty"`
	HostConfig   hostConfig          `json:"HostConfig"`
}

type hostConfig struct {
	NetworkMode string   `json:"NetworkMode"`
	Privileged  bool     `json:"Privileged"`
	NanoCPUs    int64    `json:"NanoCpus"`
	Memory      int64    `json:"Memory"`
	CapAdd      []string `json:"CapAdd,omitempty"`
	AutoRemove  bool     `json:"AutoRemove"`
	// No published ports: targets are only reachable from inside the range.
}

func (d *dockerAPI) createContainer(ctx context.Context, name string, req containerCreateRequest) (string, error) {
	var out struct {
		ID string `json:"Id"`
	}
	path := "/containers/create?name=" + url.QueryEscape(name)
	if err := d.doJSON(ctx, http.MethodPost, path, req, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

func (d *dockerAPI) startContainer(ctx context.Context, id string) error {
	res, err := d.do(ctx, http.MethodPost, "/containers/"+id+"/start", nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 && res.StatusCode != http.StatusNotModified {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return fmt.Errorf("start container %d: %s", res.StatusCode, string(msg))
	}
	return nil
}

func (d *dockerAPI) removeContainer(ctx context.Context, id string) error {
	res, err := d.do(ctx, http.MethodDelete, "/containers/"+id+"?force=true&v=true", nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 && res.StatusCode != http.StatusNotFound {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return fmt.Errorf("remove container %d: %s", res.StatusCode, string(msg))
	}
	return nil
}

type containerInspect struct {
	ID              string `json:"Id"`
	NetworkSettings struct {
		Networks map[string]struct {
			IPAddress string `json:"IPAddress"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
	State struct {
		Running bool `json:"Running"`
	} `json:"State"`
}

func (d *dockerAPI) inspect(ctx context.Context, id string) (*containerInspect, error) {
	var out containerInspect
	if err := d.doJSON(ctx, http.MethodGet, "/containers/"+id+"/json", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (d *dockerAPI) imageExists(ctx context.Context, image string) bool {
	res, err := d.do(ctx, http.MethodGet, "/images/"+url.PathEscape(image)+"/json", nil)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	return res.StatusCode == http.StatusOK
}

func (d *dockerAPI) pullImage(ctx context.Context, image string) error {
	ref := image
	if !strings.Contains(image, ":") {
		ref = image + ":latest"
	}
	res, err := d.do(ctx, http.MethodPost, "/images/create?fromImage="+url.QueryEscape(ref), nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return fmt.Errorf("pull image %s: %d %s", ref, res.StatusCode, string(msg))
	}
	// Drain the progress stream so the pull completes before we return.
	_, _ = io.Copy(io.Discard, res.Body)
	return nil
}

// ---------------------------------------------------------------------- exec

func (d *dockerAPI) exec(ctx context.Context, containerID string, cmd []string) (*ExecResult, error) {
	var created struct {
		ID string `json:"Id"`
	}
	createReq := map[string]any{
		"AttachStdout": true,
		"AttachStderr": true,
		"Tty":          false,
		"Cmd":          cmd,
	}
	if err := d.doJSON(ctx, http.MethodPost, "/containers/"+containerID+"/exec", createReq, &created); err != nil {
		return nil, err
	}

	res, err := d.do(ctx, http.MethodPost, "/exec/"+created.ID+"/start", map[string]any{"Detach": false, "Tty": false})
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return nil, fmt.Errorf("exec start %d: %s", res.StatusCode, string(msg))
	}
	stdout, stderr, err := demuxDockerStream(res.Body)
	if err != nil {
		return nil, err
	}

	var inspectOut struct {
		ExitCode int  `json:"ExitCode"`
		Running  bool `json:"Running"`
	}
	if err := d.doJSON(ctx, http.MethodGet, "/exec/"+created.ID+"/json", nil, &inspectOut); err != nil {
		return nil, err
	}
	return &ExecResult{Stdout: stdout, Stderr: stderr, ExitCode: inspectOut.ExitCode}, nil
}

// demuxDockerStream parses Docker's multiplexed stdout/stderr stream framing
// (8-byte header: [stream type, 0,0,0, uint32 size]).
func demuxDockerStream(r io.Reader) (string, string, error) {
	var stdout, stderr bytes.Buffer
	header := make([]byte, 8)
	for {
		if _, err := io.ReadFull(r, header); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return stdout.String(), stderr.String(), err
		}
		size := binary.BigEndian.Uint32(header[4:8])
		if size == 0 {
			continue
		}
		payload := make([]byte, size)
		if _, err := io.ReadFull(r, payload); err != nil {
			return stdout.String(), stderr.String(), err
		}
		switch header[0] {
		case 2:
			stderr.Write(payload)
		default:
			stdout.Write(payload)
		}
	}
	return stdout.String(), stderr.String(), nil
}

// ----------------------------------------------------------------- stats

type statsResponse struct {
	Name     string `json:"name"`
	CPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs  uint32 `json:"online_cpus"`
	} `json:"cpu_stats"`
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemUsage uint64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
	MemoryStats struct {
		Usage uint64 `json:"usage"`
		Limit uint64 `json:"limit"`
	} `json:"memory_stats"`
}

func (d *dockerAPI) stats(ctx context.Context, id string) (*ContainerStats, error) {
	var sr statsResponse
	if err := d.doJSON(ctx, http.MethodGet, "/containers/"+id+"/stats?stream=false&one-shot=true", nil, &sr); err != nil {
		return nil, err
	}
	cpuDelta := float64(sr.CPUStats.CPUUsage.TotalUsage) - float64(sr.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(sr.CPUStats.SystemUsage) - float64(sr.PreCPUStats.SystemUsage)
	cpuPct := 0.0
	if sysDelta > 0 && cpuDelta > 0 {
		cpus := float64(sr.CPUStats.OnlineCPUs)
		if cpus == 0 {
			cpus = 1
		}
		cpuPct = (cpuDelta / sysDelta) * cpus * 100.0
	}
	return &ContainerStats{
		Name:       strings.TrimPrefix(sr.Name, "/"),
		CPUPercent: cpuPct,
		MemUsedMB:  float64(sr.MemoryStats.Usage) / (1024 * 1024),
		MemLimitMB: float64(sr.MemoryStats.Limit) / (1024 * 1024),
	}, nil
}
