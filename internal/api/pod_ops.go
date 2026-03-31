package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"

	"multikube-manager/internal/kube"
	"multikube-manager/internal/model"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type podExecPayload struct {
	Container string `json:"container"`
	Command   string `json:"command"`
}

type workloadRollbackPayload struct {
	Revision int64 `json:"revision"`
}

var terminalUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (s *Server) listWorkloadPods(c *gin.Context) {
	_, runtime, namespace, resourceType, resourceName, ok := s.loadPodOperationContext(c)
	if !ok {
		return
	}

	items, err := kube.ListWorkloadPods(c.Request.Context(), runtime, resourceType, namespace, resourceName)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	respondData(c, http.StatusOK, items)
}

func (s *Server) listWorkloadRelations(c *gin.Context) {
	_, runtime, namespace, resourceType, resourceName, ok := s.loadPodOperationContext(c)
	if !ok {
		return
	}

	items, err := kube.ListWorkloadRelations(c.Request.Context(), runtime, resourceType, namespace, resourceName)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	respondData(c, http.StatusOK, items)
}

func (s *Server) getWorkloadHistory(c *gin.Context) {
	_, runtime, namespace, resourceType, resourceName, ok := s.loadPodOperationContext(c)
	if !ok {
		return
	}

	items, err := kube.ListWorkloadHistory(c.Request.Context(), runtime, resourceType, namespace, resourceName)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	respondData(c, http.StatusOK, items)
}

func (s *Server) rollbackWorkload(c *gin.Context) {
	_, runtime, namespace, resourceType, resourceName, ok := s.loadPodOperationContext(c)
	if !ok {
		return
	}

	var input workloadRollbackPayload
	if err := c.ShouldBindJSON(&input); err != nil || input.Revision <= 0 {
		respondError(c, http.StatusBadRequest, "revision is required")
		return
	}

	message, err := kube.RollbackWorkload(
		c.Request.Context(),
		runtime,
		resourceType,
		namespace,
		resourceName,
		input.Revision,
	)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	respondData(c, http.StatusOK, gin.H{
		"resourceType": resourceType,
		"name":         resourceName,
		"namespace":    namespace,
		"revision":     input.Revision,
		"message":      message,
	})
}

func (s *Server) getPodLogs(c *gin.Context) {
	_, runtime, namespace, ok := s.loadPodContext(c)
	if !ok {
		return
	}

	tailLines := int64(300)
	if raw := strings.TrimSpace(c.Query("tailLines")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 {
			respondError(c, http.StatusBadRequest, "tailLines is invalid")
			return
		}
		tailLines = parsed
	}

	podName := strings.TrimSpace(c.Param("name"))
	container := strings.TrimSpace(c.Query("container"))

	content, err := kube.GetPodLogs(c.Request.Context(), runtime, namespace, podName, container, tailLines)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	respondData(c, http.StatusOK, gin.H{
		"pod":       podName,
		"namespace": namespace,
		"container": container,
		"content":   content,
	})
}

func (s *Server) streamPodLogs(c *gin.Context) {
	_, runtime, namespace, ok := s.loadPodContext(c)
	if !ok {
		return
	}

	tailLines := int64(200)
	if raw := strings.TrimSpace(c.Query("tailLines")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 {
			respondError(c, http.StatusBadRequest, "tailLines is invalid")
			return
		}
		tailLines = parsed
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		respondError(c, http.StatusInternalServerError, "streaming is not supported")
		return
	}

	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	err := kube.StreamPodLogs(
		c.Request.Context(),
		runtime,
		namespace,
		strings.TrimSpace(c.Param("name")),
		strings.TrimSpace(c.Query("container")),
		tailLines,
		c.Writer,
		flusher.Flush,
	)
	if err != nil && !errors.Is(err, context.Canceled) {
		if _, writeErr := io.WriteString(c.Writer, "\n"+err.Error()+"\n"); writeErr == nil {
			flusher.Flush()
		}
	}
}

func (s *Server) execPodCommand(c *gin.Context) {
	_, runtime, namespace, ok := s.loadPodContext(c)
	if !ok {
		return
	}

	var input podExecPayload
	if err := c.ShouldBindJSON(&input); err != nil || strings.TrimSpace(input.Command) == "" {
		respondError(c, http.StatusBadRequest, "command is required")
		return
	}

	podName := strings.TrimSpace(c.Param("name"))
	result, err := kube.ExecPodCommand(
		c.Request.Context(),
		runtime,
		namespace,
		podName,
		strings.TrimSpace(input.Container),
		strings.TrimSpace(input.Command),
	)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	respondData(c, http.StatusOK, gin.H{
		"pod":       podName,
		"namespace": namespace,
		"container": strings.TrimSpace(input.Container),
		"command":   strings.TrimSpace(input.Command),
		"stdout":    result.Stdout,
		"stderr":    result.Stderr,
	})
}

func (s *Server) openPodTerminal(c *gin.Context) {
	_, runtime, namespace, ok := s.loadPodContext(c)
	if !ok {
		return
	}

	conn, err := terminalUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	messenger := &podTerminalMessenger{conn: conn}
	if err := messenger.Write(kube.TerminalMessage{Type: "status", Data: "connecting"}); err != nil {
		return
	}
	if err := kube.StreamPodTerminal(
		c.Request.Context(),
		runtime,
		namespace,
		strings.TrimSpace(c.Param("name")),
		strings.TrimSpace(c.Query("container")),
		messenger,
	); err != nil && !errors.Is(err, context.Canceled) {
		_ = messenger.Write(kube.TerminalMessage{Type: "error", Error: err.Error()})
		return
	}
	_ = messenger.Write(kube.TerminalMessage{Type: "status", Data: "closed"})
}

func (s *Server) uploadPodFile(c *gin.Context) {
	_, runtime, namespace, ok := s.loadPodContext(c)
	if !ok {
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		respondError(c, http.StatusBadRequest, "file is required")
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		respondError(c, http.StatusBadRequest, "打开上传文件失败")
		return
	}
	defer file.Close()

	container := strings.TrimSpace(c.PostForm("container"))
	destinationDir := strings.TrimSpace(c.PostForm("destinationDir"))

	if err := kube.UploadPodFile(
		c.Request.Context(),
		runtime,
		namespace,
		strings.TrimSpace(c.Param("name")),
		container,
		destinationDir,
		fileHeader.Filename,
		file,
	); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	respondData(c, http.StatusOK, gin.H{
		"namespace":      namespace,
		"pod":            strings.TrimSpace(c.Param("name")),
		"container":      container,
		"destinationDir": destinationDir,
		"fileName":       fileHeader.Filename,
	})
}

func (s *Server) downloadPodFile(c *gin.Context) {
	_, runtime, namespace, ok := s.loadPodContext(c)
	if !ok {
		return
	}

	remotePath := strings.TrimSpace(c.Query("path"))
	if remotePath == "" {
		respondError(c, http.StatusBadRequest, "path is required")
		return
	}

	content, err := kube.DownloadPodFile(
		c.Request.Context(),
		runtime,
		namespace,
		strings.TrimSpace(c.Param("name")),
		strings.TrimSpace(c.Query("container")),
		remotePath,
	)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	fileName := path.Base(remotePath)
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName))
	c.Data(http.StatusOK, http.DetectContentType(content), content)
}

func (s *Server) loadPodOperationContext(
	c *gin.Context,
) (*model.Cluster, *kube.Runtime, string, string, string, bool) {
	cluster, runtime, namespace, ok := s.loadPodContext(c)
	if !ok {
		return nil, nil, "", "", "", false
	}

	resourceType := strings.TrimSpace(c.Param("resourceType"))
	resourceName := strings.TrimSpace(c.Param("name"))
	if !kube.SupportsPodOperations(resourceType) {
		respondError(c, http.StatusBadRequest, "当前资源类型暂不支持 Pod 操作")
		return nil, nil, "", "", "", false
	}
	if resourceName == "" {
		respondError(c, http.StatusBadRequest, "resource name is required")
		return nil, nil, "", "", "", false
	}

	return cluster, runtime, namespace, resourceType, resourceName, true
}

func (s *Server) loadPodContext(c *gin.Context) (*model.Cluster, *kube.Runtime, string, bool) {
	cluster, err := s.loadClusterFromParam(c)
	if err != nil {
		return nil, nil, "", false
	}

	namespace := strings.TrimSpace(c.Query("namespace"))
	if namespace == "" {
		respondError(c, http.StatusBadRequest, "namespace is required")
		return nil, nil, "", false
	}

	runtime, err := s.kubeFactory.Runtime(cluster)
	if err != nil {
		s.markClusterError(cluster, err)
		respondError(c, http.StatusBadRequest, err.Error())
		return nil, nil, "", false
	}

	return cluster, runtime, namespace, true
}

type podTerminalMessenger struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (m *podTerminalMessenger) Read() (kube.TerminalMessage, error) {
	var message kube.TerminalMessage
	err := m.conn.ReadJSON(&message)
	return message, err
}

func (m *podTerminalMessenger) Write(message kube.TerminalMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.conn.WriteJSON(message)
}
