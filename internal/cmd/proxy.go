package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lukemelnik/grove/internal/certs"
	"github.com/lukemelnik/grove/internal/config"
	"github.com/lukemelnik/grove/internal/proxy"

	"github.com/spf13/cobra"
)

const (
	pidFileName  = "proxy.pid"
	portFileName = "proxy.port"
	logFileName  = "proxy.log"
)

func newProxyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "proxy",
		Short: "Manage the grove HTTPS reverse proxy daemon",
		Long: `The grove proxy maps <service>.<branch>.<project>.localhost hostnames
to deterministic ports, with auto-generated TLS certificates for HTTPS/HTTP2.

A single proxy daemon serves all grove projects on this machine.

Commands:
  grove proxy start              Start proxy in foreground
  grove proxy start -d           Start proxy as background daemon
  grove proxy stop               Stop the proxy daemon
  grove proxy status             Show proxy state, registered projects, and active routes
  grove proxy projects           List registered projects
  grove proxy unregister [name]  Remove a project from the proxy
  grove proxy clean              Stop proxy and remove all state`,
	}

	cmd.AddCommand(
		newProxyStartCmd(),
		newProxyStopCmd(),
		newProxyStatusCmd(),
		newProxyProjectsCmd(),
		newProxyUnregisterCmd(),
		newProxyCleanCmd(),
	)

	return cmd
}

func newProxyStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the reverse proxy",
		Long: `Start the HTTPS reverse proxy. By default runs in the foreground.
Use --daemon / -d to detach and run in the background.

Does not require being in a grove project directory — reads registered
projects from ~/.grove/proxy/projects.json.`,
		Args: cobra.NoArgs,
		RunE: runProxyStart,
	}

	cmd.Flags().BoolP("daemon", "d", false, "run as a background daemon")
	cmd.Flags().Int("port", 0, "proxy listen port (default: 1355)")
	cmd.Flags().Bool("no-https", false, "disable HTTPS (use plain HTTP)")
	cmd.Flags().Bool("json", false, "output as JSON (agent-friendly)")

	return cmd
}

func newProxyStopCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the proxy daemon",
		Args:  cobra.NoArgs,
		RunE:  runProxyStop,
	}
	cmd.Flags().Bool("json", false, "output as JSON (agent-friendly)")
	return cmd
}

func newProxyStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show proxy status, registered projects, and active routes",
		Args:  cobra.NoArgs,
		RunE:  runProxyStatus,
	}
	cmd.Flags().Bool("json", false, "output as JSON (agent-friendly)")
	return cmd
}

func newProxyProjectsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "projects",
		Short: "List registered projects",
		Args:  cobra.NoArgs,
		RunE:  runProxyProjects,
	}
	cmd.Flags().Bool("json", false, "output as JSON (agent-friendly)")
	return cmd
}

func newProxyUnregisterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unregister [name]",
		Short: "Remove a project from the proxy registry",
		Long: `Remove a project from the proxy registry.

Without arguments, removes the project for the current directory.
With a name argument, removes the project with that name.`,
		Args: cobra.MaximumNArgs(1),
		RunE: runProxyUnregister,
	}
	cmd.Flags().Bool("json", false, "output as JSON (agent-friendly)")
	return cmd
}

func newProxyCleanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Stop proxy and remove all proxy state",
		Long: `Stop the proxy daemon (if running) and remove all proxy state
from ~/.grove/proxy/ including certificates, registry, and PID file.`,
		Args: cobra.NoArgs,
		RunE: runProxyClean,
	}
	cmd.Flags().Bool("json", false, "output as JSON (agent-friendly)")
	return cmd
}

func proxyStateDir() (string, error) {
	return certs.DefaultStateDir()
}

func readPIDFile(stateDir string) (int, error) {
	data, err := os.ReadFile(filepath.Join(stateDir, pidFileName))
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid PID file content: %w", err)
	}
	return pid, nil
}

func isProxyRunning(stateDir string) (bool, int) {
	pid, err := readPIDFile(stateDir)
	if err != nil {
		return false, 0
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false, 0
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return false, 0
	}
	return true, pid
}

func readPortFile(stateDir string) (int, error) {
	data, err := os.ReadFile(filepath.Join(stateDir, portFileName))
	if err != nil {
		return 0, err
	}
	port, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid port file content: %w", err)
	}
	return port, nil
}

func writePIDFile(stateDir string, pid int) error {
	return os.WriteFile(filepath.Join(stateDir, pidFileName), []byte(strconv.Itoa(pid)+"\n"), 0644)
}

func writePortFile(stateDir string, port int) error {
	return os.WriteFile(filepath.Join(stateDir, portFileName), []byte(strconv.Itoa(port)+"\n"), 0644)
}

func cleanPIDFile(stateDir string) {
	os.Remove(filepath.Join(stateDir, pidFileName))
}

func cleanPortFile(stateDir string) {
	os.Remove(filepath.Join(stateDir, portFileName))
}

func runProxyStart(cmd *cobra.Command, _ []string) error {
	daemon, _ := cmd.Flags().GetBool("daemon")
	portFlag, _ := cmd.Flags().GetInt("port")
	noHTTPS, _ := cmd.Flags().GetBool("no-https")
	jsonOutput := shouldOutputJSON(cmd)

	stateDir, err := proxyStateDir()
	if err != nil {
		return outputError(cmd, err)
	}

	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return outputError(cmd, fmt.Errorf("creating state directory: %w", err))
	}

	if running, pid := isProxyRunning(stateDir); running {
		existingPort, _ := readPortFile(stateDir)
		msg := fmt.Sprintf("proxy is already running (PID %d, port %d)", pid, existingPort)
		if jsonOutput {
			data, _ := json.Marshal(map[string]string{"message": msg})
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		}
		fmt.Fprintln(cmd.OutOrStdout(), msg)
		return nil
	}

	// Clean stale PID file
	cleanPIDFile(stateDir)

	listenPort := proxy.DefaultProxyPort
	if portFlag != 0 {
		listenPort = portFlag
	}

	enableHTTPS := !noHTTPS

	if daemon {
		return startProxyDaemon(cmd, stateDir, listenPort, enableHTTPS, jsonOutput)
	}

	return runProxyForeground(cmd, stateDir, listenPort, enableHTTPS, jsonOutput)
}

var findExecutable = os.Executable

func startProxyDaemon(cmd *cobra.Command, stateDir string, port int, enableHTTPS bool, jsonOutput bool) error {
	exe, err := findExecutable()
	if err != nil {
		return outputError(cmd, fmt.Errorf("finding grove executable: %w", err))
	}

	args := []string{"proxy", "start", "--port", strconv.Itoa(port)}
	if !enableHTTPS {
		args = append(args, "--no-https")
	}

	logPath := filepath.Join(stateDir, logFileName)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return outputError(cmd, fmt.Errorf("opening log file: %w", err))
	}

	child := exec.Command(exe, args...)
	child.Stdout = logFile
	child.Stderr = logFile
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := child.Start(); err != nil {
		logFile.Close()
		return outputError(cmd, fmt.Errorf("starting proxy daemon: %w", err))
	}

	logFile.Close()

	time.Sleep(200 * time.Millisecond)

	if jsonOutput {
		data, _ := json.Marshal(map[string]interface{}{
			"action":  "started",
			"pid":     child.Process.Pid,
			"port":    port,
			"https":   enableHTTPS,
			"message": fmt.Sprintf("proxy daemon started (PID %d, port %d)", child.Process.Pid, port),
		})
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	}

	proto := "https"
	if !enableHTTPS {
		proto = "http"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Proxy daemon started (PID %d)\n", child.Process.Pid)
	fmt.Fprintf(cmd.OutOrStdout(), "Listening on %s://127.0.0.1:%d\n", proto, port)
	fmt.Fprintf(cmd.OutOrStdout(), "Log: %s\n", logPath)
	return nil
}

func runProxyForeground(cmd *cobra.Command, stateDir string, port int, enableHTTPS bool, jsonOutput bool) error {
	listenAddr := fmt.Sprintf("127.0.0.1:%d", port)

	if port < 1024 && os.Getuid() != 0 {
		return outputError(cmd, fmt.Errorf("port %d requires root privileges — run with sudo or use a port >= 1024 (default: %d)", port, proxy.DefaultProxyPort))
	}

	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return outputError(cmd, fmt.Errorf("cannot bind to %s — is another process using this port? (check with: lsof -i :%d)", listenAddr, port))
	}

	registry := proxy.NewRegistry(stateDir)
	routeTable := proxy.NewRouteTable()

	entries, err := registry.LoadAndPrune()
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: loading registry: %v\n", err)
	}

	routes, err := proxy.ComputeAllRoutes(entries)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: computing routes: %v\n", err)
	}
	routeTable.Update(routes)

	var certMgr *certs.Manager
	if enableHTTPS {
		certMgr, err = certs.EnsureCerts(stateDir)
		if err != nil {
			return outputError(cmd, fmt.Errorf("setting up TLS certificates: %w", err))
		}
	}

	srv := proxy.NewServer(proxy.ServerConfig{
		ListenAddr:  listenAddr,
		TLSEnabled:  enableHTTPS,
		CertManager: certMgr,
		RouteTable:  routeTable,
	})

	if err := writePIDFile(stateDir, os.Getpid()); err != nil {
		return outputError(cmd, fmt.Errorf("writing PID file: %w", err))
	}
	if err := writePortFile(stateDir, port); err != nil {
		return outputError(cmd, fmt.Errorf("writing port file: %w", err))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go watchAndRebuildRoutes(ctx, registry, routeTable, stateDir)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		<-sigCh
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		srv.Shutdown(shutdownCtx)
	}()

	proto := "https"
	if !enableHTTPS {
		proto = "http"
	}

	if jsonOutput {
		data, _ := json.Marshal(map[string]interface{}{
			"action":  "started",
			"pid":     os.Getpid(),
			"port":    port,
			"https":   enableHTTPS,
			"routes":  len(routes),
			"message": fmt.Sprintf("proxy started on %s://127.0.0.1:%d", proto, port),
		})
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Proxy started on %s://127.0.0.1:%d (PID %d)\n", proto, port, os.Getpid())
		fmt.Fprintf(cmd.OutOrStdout(), "Active routes: %d\n", len(routes))
	}

	err = srv.Serve(ln)

	cleanPIDFile(stateDir)
	cleanPortFile(stateDir)

	if err != nil && ctx.Err() != nil {
		if !jsonOutput {
			fmt.Fprintln(cmd.OutOrStdout(), "Proxy stopped")
		}
		return nil
	}
	return err
}

func watchAndRebuildRoutes(ctx context.Context, registry *proxy.Registry, routeTable *proxy.RouteTable, stateDir string) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var lastHash string

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hash := computeWatchHash(stateDir, registry)
			if hash == lastHash {
				continue
			}
			lastHash = hash

			entries, err := registry.LoadAndPrune()
			if err != nil {
				continue
			}
			routes, err := proxy.ComputeAllRoutes(entries)
			if err != nil {
				continue
			}
			routeTable.Update(routes)
		}
	}
}

func computeWatchHash(stateDir string, registry *proxy.Registry) string {
	var sb strings.Builder

	registryPath := filepath.Join(stateDir, "projects.json")
	if info, err := os.Stat(registryPath); err == nil {
		sb.WriteString(info.ModTime().String())
	}

	entries, err := registry.ListProjects()
	if err != nil {
		return sb.String()
	}

	for _, entry := range entries {
		configPath := filepath.Join(entry.Path, config.ConfigFileName)
		if info, err := os.Stat(configPath); err == nil {
			sb.WriteString(entry.Path)
			sb.WriteString(info.ModTime().String())
		}

		cfg, err := config.LoadNoValidate(configPath)
		if err != nil {
			continue
		}

		wtDir := cfg.WorktreeDir
		if !filepath.IsAbs(wtDir) {
			wtDir = filepath.Join(entry.Path, wtDir)
		}
		if info, err := os.Stat(wtDir); err == nil {
			sb.WriteString(info.ModTime().String())
		}
	}

	return sb.String()
}

func runProxyStop(cmd *cobra.Command, _ []string) error {
	jsonOutput := shouldOutputJSON(cmd)

	stateDir, err := proxyStateDir()
	if err != nil {
		return outputError(cmd, err)
	}

	running, pid := isProxyRunning(stateDir)
	if !running {
		cleanPIDFile(stateDir)
		msg := "proxy is not running"
		if jsonOutput {
			data, _ := json.Marshal(map[string]string{"message": msg})
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		}
		fmt.Fprintln(cmd.OutOrStdout(), msg)
		return nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return outputError(cmd, fmt.Errorf("finding process %d: %w", pid, err))
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		cleanPIDFile(stateDir)
		return outputError(cmd, fmt.Errorf("sending SIGTERM to PID %d: %w", pid, err))
	}

	for i := 0; i < 30; i++ {
		time.Sleep(100 * time.Millisecond)
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			break
		}
	}

	cleanPIDFile(stateDir)
	cleanPortFile(stateDir)

	if jsonOutput {
		data, _ := json.Marshal(map[string]interface{}{
			"action":  "stopped",
			"pid":     pid,
			"message": fmt.Sprintf("proxy stopped (PID %d)", pid),
		})
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Proxy stopped (PID %d)\n", pid)
	return nil
}

type proxyStatusOutput struct {
	Running  bool                 `json:"running"`
	PID      int                  `json:"pid,omitempty"`
	Port     int                  `json:"port,omitempty"`
	HTTPS    bool                 `json:"https"`
	Projects []proxy.ProjectEntry `json:"projects"`
	Routes   []routeOutput        `json:"routes"`
}

type routeOutput struct {
	Hostname string `json:"hostname"`
	Target   string `json:"target"`
	Project  string `json:"project"`
	Service  string `json:"service"`
	Branch   string `json:"branch"`
}

func runProxyStatus(cmd *cobra.Command, _ []string) error {
	jsonOutput := shouldOutputJSON(cmd)

	stateDir, err := proxyStateDir()
	if err != nil {
		return outputError(cmd, err)
	}

	running, pid := isProxyRunning(stateDir)
	port, _ := readPortFile(stateDir)

	registry := proxy.NewRegistry(stateDir)
	entries, _ := registry.LoadAndPrune()

	var routes []proxy.Route
	if len(entries) > 0 {
		routes, _ = proxy.ComputeAllRoutes(entries)
	}

	// HTTPS state is not persisted — default to true since that's the proxy default.
	// Only the running proxy knows for sure; this is a best-effort status display.
	httpsEnabled := true

	if jsonOutput {
		routeOutputs := make([]routeOutput, 0, len(routes))
		for _, r := range routes {
			routeOutputs = append(routeOutputs, routeOutput{
				Hostname: r.Hostname,
				Target:   r.Target,
				Project:  r.Project,
				Service:  r.Service,
				Branch:   r.Branch,
			})
		}
		if entries == nil {
			entries = []proxy.ProjectEntry{}
		}
		out := proxyStatusOutput{
			Running:  running,
			PID:      pid,
			Port:     port,
			HTTPS:    httpsEnabled,
			Projects: entries,
			Routes:   routeOutputs,
		}
		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	}

	w := cmd.OutOrStdout()
	if running {
		fmt.Fprintf(w, "Status:   running (PID %d)\n", pid)
		fmt.Fprintf(w, "Port:     %d\n", port)
		fmt.Fprintf(w, "TLS:      %v\n", httpsEnabled)
	} else {
		fmt.Fprintln(w, "Status:   not running")
	}

	fmt.Fprintf(w, "Projects: %d\n", len(entries))
	for _, entry := range entries {
		fmt.Fprintf(w, "  %s (%s)\n", entry.Name, entry.Path)
	}

	fmt.Fprintf(w, "Routes:   %d\n", len(routes))
	for _, r := range routes {
		fmt.Fprintf(w, "  %s → %s\n", r.Hostname, r.Target)
	}

	return nil
}

func runProxyProjects(cmd *cobra.Command, _ []string) error {
	jsonOutput := shouldOutputJSON(cmd)

	stateDir, err := proxyStateDir()
	if err != nil {
		return outputError(cmd, err)
	}

	registry := proxy.NewRegistry(stateDir)
	entries, err := registry.LoadAndPrune()
	if err != nil {
		return outputError(cmd, fmt.Errorf("loading registry: %w", err))
	}

	if jsonOutput {
		if entries == nil {
			entries = []proxy.ProjectEntry{}
		}
		data, _ := json.MarshalIndent(entries, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	}

	w := cmd.OutOrStdout()
	if len(entries) == 0 {
		fmt.Fprintln(w, "No projects registered")
		return nil
	}

	for _, entry := range entries {
		fmt.Fprintf(w, "%s\n  Path: %s\n", entry.Name, entry.Path)
	}
	return nil
}

func runProxyUnregister(cmd *cobra.Command, args []string) error {
	jsonOutput := shouldOutputJSON(cmd)

	stateDir, err := proxyStateDir()
	if err != nil {
		return outputError(cmd, err)
	}

	registry := proxy.NewRegistry(stateDir)

	if len(args) > 0 {
		name := args[0]
		if err := registry.UnregisterByName(name); err != nil {
			return outputError(cmd, err)
		}
		if jsonOutput {
			data, _ := json.Marshal(map[string]string{
				"action":  "unregistered",
				"name":    name,
				"message": fmt.Sprintf("project %q removed from proxy registry", name),
			})
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Project %q removed from proxy registry\n", name)
		return nil
	}

	cwd, err := getWorkingDir()
	if err != nil {
		return outputError(cmd, fmt.Errorf("getting working directory: %w", err))
	}

	_, projectRoot, err := config.Discover(cwd)
	if err != nil {
		return outputError(cmd, fmt.Errorf("not inside a grove project: %w", err))
	}

	if err := registry.UnregisterProject(projectRoot); err != nil {
		return outputError(cmd, err)
	}

	projectName := filepath.Base(filepath.Clean(projectRoot))
	if jsonOutput {
		data, _ := json.Marshal(map[string]string{
			"action":  "unregistered",
			"path":    projectRoot,
			"message": fmt.Sprintf("project at %s removed from proxy registry", projectRoot),
		})
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Project %q at %s removed from proxy registry\n", projectName, projectRoot)
	return nil
}

func runProxyClean(cmd *cobra.Command, _ []string) error {
	jsonOutput := shouldOutputJSON(cmd)

	stateDir, err := proxyStateDir()
	if err != nil {
		return outputError(cmd, err)
	}

	if running, pid := isProxyRunning(stateDir); running {
		proc, findErr := os.FindProcess(pid)
		if findErr == nil {
			proc.Signal(syscall.SIGTERM)
			time.Sleep(500 * time.Millisecond)
		}
	}

	if err := os.RemoveAll(stateDir); err != nil {
		return outputError(cmd, fmt.Errorf("removing proxy state directory: %w", err))
	}

	if jsonOutput {
		data, _ := json.Marshal(map[string]string{
			"action":  "cleaned",
			"path":    stateDir,
			"message": "proxy state removed — run 'grove trust --remove' to also remove the CA from your keychain",
		})
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Removed proxy state at %s\n", stateDir)
	fmt.Fprintln(cmd.OutOrStdout(), "Run 'grove trust --remove' to also remove the CA from your keychain")
	return nil
}
