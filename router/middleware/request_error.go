package middleware

import (
	"context"
	"net/http"
	"os"
	"strings"

	"emperror.dev/errors"
	"github.com/apex/log"
	"github.com/gin-gonic/gin"

	"github.com/pwindows/phantom-wings/server"
	"github.com/pwindows/phantom-wings/server/filesystem"
)

type RequestError struct {
	err    error
	status int
	msg    string
}

func NewError(err error) *RequestError {
	return &RequestError{
		err: errors.WithStackDepthIf(err, 1),
	}
}

func (re *RequestError) SetMessage(m string) {
	re.msg = m
}

func (re *RequestError) SetStatus(s int) {
	re.status = s
}

func (re *RequestError) Abort(c *gin.Context, status int) {
	reqId := c.Writer.Header().Get("X-Request-Id")

	event := log.WithField("request_id", reqId).WithField("url", c.Request.URL.String())
	if s, ok := c.Get("server"); ok {
		if s, ok := s.(*server.Server); ok {
			event = event.WithField("server_id", s.ID())
		}
	}

	if c.Writer.Status() == 200 {
		if errors.Is(re.err, context.DeadlineExceeded) {
			re.SetStatus(http.StatusGatewayTimeout)
			re.SetMessage("The server could not process this request in time, please try again.")
		} else if strings.Contains(re.Cause().Error(), "context canceled") {
			re.SetStatus(http.StatusBadRequest)
			re.SetMessage("Request aborted by client.")
		}
	}

	if status >= 500 || c.Writer.Status() != 200 {
		event.WithField("status", status).WithField("error", re.err).Error("error while handling HTTP request")
	} else {
		event.WithField("status", status).WithField("error", re.err).Debug("error handling HTTP request (not a server error)")
	}
	if re.msg == "" {
		re.msg = "An unexpected error was encountered while processing this request"
	}
	c.AbortWithStatusJSON(status, gin.H{"error": re.msg, "request_id": reqId})
}

func (re *RequestError) Cause() error {
	return re.err
}

func (re *RequestError) Error() string {
	return re.err.Error()
}

func (re *RequestError) asFilesystemError() (int, string) {
	err := re.Cause()
	if err == nil {
		return 0, ""
	}
	if filesystem.IsErrorCode(err, filesystem.ErrNotExist) ||
		filesystem.IsErrorCode(err, filesystem.ErrCodePathResolution) || strings.Contains(err.Error(), "file does not exist") ||
		strings.Contains(err.Error(), "resolves to a location outside the server root") {
		return http.StatusNotFound, "The requested resources was not found on the system."
	}
	if filesystem.IsErrorCode(err, filesystem.ErrCodeDenylistFile) || strings.Contains(err.Error(), "filesystem: file access prohibited") {
		return http.StatusForbidden, "This file cannot be modified: present in egg denylist."
	}
	if filesystem.IsErrorCode(err, filesystem.ErrCodeIsDirectory) || strings.Contains(err.Error(), "filesystem: is a directory") {
		return http.StatusBadRequest, "Cannot perform that action: file is a directory."
	}
	if filesystem.IsErrorCode(err, filesystem.ErrCodeDiskSpace) || strings.Contains(err.Error(), "filesystem: not enough disk space") {
		return http.StatusBadRequest, "There is not enough disk space available to perform that action."
	}
	if strings.HasSuffix(err.Error(), "file name too long") {
		return http.StatusBadRequest, "Cannot perform that action: file name is too long."
	}
	if e, ok := err.(*os.SyscallError); ok && e.Syscall == "readdirent" {
		return http.StatusNotFound, "The requested directory does not exist."
	}
	return 0, ""
}