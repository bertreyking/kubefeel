package kube

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

type TerminalMessage struct {
	Type   string `json:"type"`
	Stream string `json:"stream,omitempty"`
	Data   string `json:"data,omitempty"`
	Error  string `json:"error,omitempty"`
	Cols   uint16 `json:"cols,omitempty"`
	Rows   uint16 `json:"rows,omitempty"`
}

type containerShellCandidate struct {
	label       string
	probe       []string
	interactive []string
	command     []string
}

func StreamPodLogs(
	ctx context.Context,
	runtime *Runtime,
	namespace, podName, container string,
	tailLines int64,
	writer io.Writer,
	flush func(),
) error {
	if strings.TrimSpace(namespace) == "" {
		return fmt.Errorf("namespace is required")
	}
	if strings.TrimSpace(podName) == "" {
		return fmt.Errorf("pod name is required")
	}

	clientset, err := streamingClientset(runtime)
	if err != nil {
		return err
	}

	options := &corev1.PodLogOptions{
		Follow:     true,
		Timestamps: true,
	}
	if tailLines > 0 {
		options.TailLines = &tailLines
	}
	if trimmed := strings.TrimSpace(container); trimmed != "" {
		options.Container = trimmed
	}

	stream, err := clientset.CoreV1().Pods(namespace).GetLogs(podName, options).Stream(ctx)
	if err != nil {
		return err
	}
	defer stream.Close()

	buffer := make([]byte, 4096)
	for {
		count, readErr := stream.Read(buffer)
		if count > 0 {
			if _, writeErr := writer.Write(buffer[:count]); writeErr != nil {
				return writeErr
			}
			if flush != nil {
				flush()
			}
		}

		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func UploadPodFile(
	ctx context.Context,
	runtime *Runtime,
	namespace, podName, container, destinationDir, fileName string,
	content io.Reader,
) error {
	if strings.TrimSpace(fileName) == "" {
		return fmt.Errorf("file name is required")
	}

	targetDir := strings.TrimSpace(destinationDir)
	if targetDir == "" {
		targetDir = "/tmp"
	}
	targetDir = path.Clean(targetDir)
	targetPath := path.Join(targetDir, path.Base(fileName))
	shell, err := resolveContainerShell(ctx, runtime, namespace, podName, container)
	if err != nil {
		return fmt.Errorf("当前容器不支持文件上传: %w", err)
	}

	_, stderr, err := streamExecCommand(
		ctx,
		runtime,
		namespace,
		podName,
		container,
		shell.commandLine(fmt.Sprintf("mkdir -p %s && cat > %s", shellQuote(targetDir), shellQuote(targetPath))),
		content,
		io.Discard,
		false,
		nil,
	)
	if err != nil {
		if stderr != "" {
			return fmt.Errorf("%s: %w", stderr, err)
		}
		return err
	}

	return nil
}

func DownloadPodFile(
	ctx context.Context,
	runtime *Runtime,
	namespace, podName, container, remotePath string,
) ([]byte, error) {
	trimmedPath := strings.TrimSpace(remotePath)
	if trimmedPath == "" {
		return nil, fmt.Errorf("path is required")
	}
	shell, err := resolveContainerShell(ctx, runtime, namespace, podName, container)
	if err != nil {
		return nil, fmt.Errorf("当前容器不支持文件下载: %w", err)
	}

	var stdout bytes.Buffer
	_, stderr, err := streamExecCommand(
		ctx,
		runtime,
		namespace,
		podName,
		container,
		shell.commandLine(fmt.Sprintf("cat %s", shellQuote(path.Clean(trimmedPath)))),
		nil,
		&stdout,
		false,
		nil,
	)
	if err != nil {
		if stderr != "" {
			return nil, fmt.Errorf("%s: %w", stderr, err)
		}
		return nil, err
	}

	return stdout.Bytes(), nil
}

func StreamPodTerminal(
	ctx context.Context,
	runtime *Runtime,
	namespace, podName, container string,
	messenger TerminalMessenger,
) error {
	shell, err := resolveContainerShell(ctx, runtime, namespace, podName, container)
	if err != nil {
		return err
	}

	stdinReader, stdinWriter := io.Pipe()
	defer stdinReader.Close()

	sizeQueue := newTerminalSizeQueue()
	done := make(chan struct{})

	go func() {
		defer close(done)
		defer stdinWriter.Close()
		for {
			message, err := messenger.Read()
			if err != nil {
				return
			}

			switch message.Type {
			case "input":
				if message.Data != "" {
					if _, writeErr := io.WriteString(stdinWriter, message.Data); writeErr != nil {
						return
					}
				}
			case "resize":
				sizeQueue.Push(message.Cols, message.Rows)
			}
		}
	}()

	stdoutWriter := &terminalOutputWriter{messenger: messenger, stream: "stdout"}
	stderrWriter := &terminalOutputWriter{messenger: messenger, stream: "stderr"}
	_, stderr, err := streamExecCommand(
		ctx,
		runtime,
		namespace,
		podName,
		container,
		shell.interactive,
		stdinReader,
		stdoutWriter,
		true,
		sizeQueue,
		stderrWriter,
	)

	select {
	case <-done:
	default:
		_ = stdinWriter.Close()
	}

	if err != nil {
		if stderr != "" {
			return fmt.Errorf("%s: %w", stderr, err)
		}
		return err
	}

	return nil
}

func availableContainerShellCandidates() []containerShellCandidate {
	return []containerShellCandidate{
		{
			label:       "/bin/sh",
			probe:       []string{"/bin/sh", "-c", "exit 0"},
			interactive: []string{"/bin/sh", "-i"},
			command:     []string{"/bin/sh", "-c"},
		},
		{
			label:       "/bin/bash",
			probe:       []string{"/bin/bash", "-c", "exit 0"},
			interactive: []string{"/bin/bash", "-i"},
			command:     []string{"/bin/bash", "-c"},
		},
		{
			label:       "/bin/ash",
			probe:       []string{"/bin/ash", "-c", "exit 0"},
			interactive: []string{"/bin/ash", "-i"},
			command:     []string{"/bin/ash", "-c"},
		},
		{
			label:       "/bin/dash",
			probe:       []string{"/bin/dash", "-c", "exit 0"},
			interactive: []string{"/bin/dash", "-i"},
			command:     []string{"/bin/dash", "-c"},
		},
		{
			label:       "/bin/zsh",
			probe:       []string{"/bin/zsh", "-c", "exit 0"},
			interactive: []string{"/bin/zsh", "-i"},
			command:     []string{"/bin/zsh", "-c"},
		},
		{
			label:       "sh",
			probe:       []string{"sh", "-c", "exit 0"},
			interactive: []string{"sh", "-i"},
			command:     []string{"sh", "-c"},
		},
		{
			label:       "bash",
			probe:       []string{"bash", "-c", "exit 0"},
			interactive: []string{"bash", "-i"},
			command:     []string{"bash", "-c"},
		},
		{
			label:       "ash",
			probe:       []string{"ash", "-c", "exit 0"},
			interactive: []string{"ash", "-i"},
			command:     []string{"ash", "-c"},
		},
		{
			label:       "dash",
			probe:       []string{"dash", "-c", "exit 0"},
			interactive: []string{"dash", "-i"},
			command:     []string{"dash", "-c"},
		},
		{
			label:       "zsh",
			probe:       []string{"zsh", "-c", "exit 0"},
			interactive: []string{"zsh", "-i"},
			command:     []string{"zsh", "-c"},
		},
		{
			label:       "busybox sh",
			probe:       []string{"busybox", "sh", "-c", "exit 0"},
			interactive: []string{"busybox", "sh", "-i"},
			command:     []string{"busybox", "sh", "-c"},
		},
	}
}

func (candidate containerShellCandidate) commandLine(command string) []string {
	next := slices.Clone(candidate.command)
	next = append(next, command)
	return next
}

func resolveContainerShell(
	ctx context.Context,
	runtime *Runtime,
	namespace, podName, container string,
) (containerShellCandidate, error) {
	candidates := availableContainerShellCandidates()
	attempted := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		_, stderr, err := streamExecCommand(
			ctx,
			runtime,
			namespace,
			podName,
			container,
			candidate.probe,
			nil,
			io.Discard,
			false,
			nil,
		)
		if err == nil {
			return candidate, nil
		}

		detail := strings.TrimSpace(strings.Join([]string{stderr, err.Error()}, " "))
		if isContainerShellMissingError(detail) {
			attempted = append(attempted, candidate.label)
			continue
		}

		return containerShellCandidate{}, err
	}

	return containerShellCandidate{}, fmt.Errorf(
		"容器内未发现可用 shell（已尝试 %s）。如果这是 distroless 或 scratch 镜像，请改用 Logs，或直接执行容器内已知二进制；需要交互排障时，建议注入临时调试容器",
		strings.Join(attempted, "、"),
	)
}

func isContainerShellMissingError(detail string) bool {
	lowered := strings.ToLower(strings.TrimSpace(detail))
	if lowered == "" {
		return false
	}

	return strings.Contains(lowered, "executable file not found") ||
		strings.Contains(lowered, "no such file or directory") ||
		strings.Contains(lowered, "not found") ||
		strings.Contains(lowered, "stat /bin/")
}

type TerminalMessenger interface {
	Read() (TerminalMessage, error)
	Write(TerminalMessage) error
}

type terminalOutputWriter struct {
	messenger TerminalMessenger
	stream    string
}

func (w *terminalOutputWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if err := w.messenger.Write(TerminalMessage{
		Type:   "output",
		Stream: w.stream,
		Data:   string(p),
	}); err != nil {
		return 0, err
	}
	return len(p), nil
}

type terminalSizeQueue struct {
	channel chan remotecommand.TerminalSize
	once    sync.Once
}

func newTerminalSizeQueue() *terminalSizeQueue {
	return &terminalSizeQueue{
		channel: make(chan remotecommand.TerminalSize, 8),
	}
}

func (q *terminalSizeQueue) Next() *remotecommand.TerminalSize {
	size, ok := <-q.channel
	if !ok {
		return nil
	}
	return &size
}

func (q *terminalSizeQueue) Push(cols, rows uint16) {
	if cols == 0 || rows == 0 {
		return
	}
	select {
	case q.channel <- remotecommand.TerminalSize{Width: cols, Height: rows}:
	default:
		select {
		case <-q.channel:
		default:
		}
		q.channel <- remotecommand.TerminalSize{Width: cols, Height: rows}
	}
}

func (q *terminalSizeQueue) Close() {
	q.once.Do(func() {
		close(q.channel)
	})
}

func streamExecCommand(
	ctx context.Context,
	runtime *Runtime,
	namespace, podName, container string,
	command []string,
	stdin io.Reader,
	stdout io.Writer,
	tty bool,
	sizeQueue remotecommand.TerminalSizeQueue,
	extraOutputs ...io.Writer,
) (string, string, error) {
	if runtime == nil || runtime.Config == nil {
		return "", "", fmt.Errorf("kubernetes config is not ready")
	}
	if strings.TrimSpace(namespace) == "" {
		return "", "", fmt.Errorf("namespace is required")
	}
	if strings.TrimSpace(podName) == "" {
		return "", "", fmt.Errorf("pod name is required")
	}

	config := rest.CopyConfig(runtime.Config)
	config.Timeout = 0

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return "", "", err
	}

	request := clientset.CoreV1().
		RESTClient().
		Post().
		Resource("pods").
		Namespace(namespace).
		Name(podName).
		SubResource("exec")

	request.VersionedParams(&corev1.PodExecOptions{
		Container: strings.TrimSpace(container),
		Command:   command,
		Stdin:     stdin != nil,
		Stdout:    stdout != nil,
		Stderr:    true,
		TTY:       tty,
	}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(config, "POST", request.URL())
	if err != nil {
		return "", "", err
	}

	var stderrBuffer bytes.Buffer
	stderrWriter := io.Writer(&stderrBuffer)
	if len(extraOutputs) > 0 {
		writers := make([]io.Writer, 0, len(extraOutputs)+1)
		writers = append(writers, &stderrBuffer)
		for _, writer := range extraOutputs {
			if writer != nil {
				writers = append(writers, writer)
			}
		}
		stderrWriter = io.MultiWriter(writers...)
	}

	var stdoutBuffer bytes.Buffer
	stdoutWriter := stdout
	if stdoutWriter == nil {
		stdoutWriter = &stdoutBuffer
	}

	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:             stdin,
		Stdout:            stdoutWriter,
		Stderr:            stderrWriter,
		Tty:               tty,
		TerminalSizeQueue: sizeQueue,
	})

	return stdoutBuffer.String(), stderrBuffer.String(), err
}

func streamingClientset(runtime *Runtime) (kubernetes.Interface, error) {
	if runtime == nil || runtime.Config == nil {
		return nil, fmt.Errorf("kubernetes config is not ready")
	}

	config := rest.CopyConfig(runtime.Config)
	config.Timeout = 0
	return kubernetes.NewForConfig(config)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
