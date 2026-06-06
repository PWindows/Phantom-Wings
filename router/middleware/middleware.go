package middleware

import (
	"crypto/subtle"
	"fmt"
	"io"
	"net/http"
	"strings"

	"emperror.dev/errors"
	"github.com/apex/log"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/pwindows/phantom-wings/config"
	"github.com/pwindows/phantom-wings/remote"
	"github.com/pwindows/phantom-wings/server"
	"github.com/pwindows/phantom-wings/system"
)

func AttachRequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := uuid.New().String()
		c.Set("request_id", id)
		c.Set("logger", log.WithField("request_id", id))
		c.Header("X-Request-Id", id)
		c.Next()
	}
}

func AttachServerManager(m *server.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("manager", m)
		c.Next()
	}
}

func AttachApiClient(client remote.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("api_client", client)
		c.Next()
	}
}

func CaptureAndAbort(c *gin.Context, err error) {
	c.Abort()
	c.Error(errors.WithStackDepthIf(err, 1))
}

func CaptureErrors() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		err := c.Errors.Last()
		if err == nil || err.Err == nil {
			return
		}

		status := http.StatusInternalServerError
		if c.Writer.Status() != 200 {
			status = c.Writer.Status()
		}
		if err.Error() == io.EOF.Error() {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "The data passed in the request was not in a parsable format. Please try again."})
			return
		}
		captured := NewError(err.Err)
		if status, msg := captured.asFilesystemError(); msg != "" {
			c.AbortWithStatusJSON(status, gin.H{"error": msg, "request_id": c.Writer.Header().Get("X-Request-Id")})
			return
		}
		captured.Abort(c, status)
	}
}

func SetAccessControlHeaders() gin.HandlerFunc {
	cfg := config.Get()
	origins := cfg.AllowedOrigins
	location := cfg.PanelLocation
	allowPrivateNetwork := cfg.AllowCORSPrivateNetwork

	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", location)
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Accept, Accept-Encoding, Authorization, Cache-Control, Content-Type, Content-Length, Origin, X-Real-IP, X-CSRF-Token")

		if allowPrivateNetwork {
			c.Header("Access-Control-Request-Private-Network", "true")
		}

		c.Header("Access-Control-Max-Age", "7200")

		origin := c.GetHeader("Origin")
		if origin != location {
			for _, o := range origins {
				if o != "*" && o != origin {
					continue
				}
				c.Header("Access-Control-Allow-Origin", o)
				break
			}
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func ServerExists() gin.HandlerFunc {
	return func(c *gin.Context) {
		var s *server.Server
		if c.Param("server") != "" {
			manager := ExtractManager(c)
			s = manager.Find(func(s *server.Server) bool {
				return c.Param("server") == s.ID()
			})
		}
		if s == nil {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "The requested resource does not exist on this instance."})
			return
		}
		c.Set("logger", ExtractLogger(c).WithField("server_id", s.ID()))
		c.Set("server", s)
		c.Next()
	}
}

func RequireAuthorization() gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := strings.SplitN(c.GetHeader("Authorization"), " ", 2)
		c.Header("User-Agent", fmt.Sprintf("Phantom Wings/v%s (id:%s)", system.Version, config.Get().AuthenticationTokenId))
		if len(auth) != 2 || auth[0] != "Bearer" {
			c.Header("WWW-Authenticate", "Bearer")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "The required authorization heads were not present in the request."})
			return
		}

		if subtle.ConstantTimeCompare([]byte(auth[1]), []byte(config.Get().Token.Token)) != 1 {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "You are not authorized to access this endpoint."})
			return
		}
		c.Next()
	}
}

func RemoteDownloadEnabled() gin.HandlerFunc {
	disabled := config.Get().Api.DisableRemoteDownload
	return func(c *gin.Context) {
		if disabled {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "This functionality is not currently enabled on this instance."})
			return
		}
		c.Next()
	}
}

func ExtractLogger(c *gin.Context) *log.Entry {
	v, ok := c.Get("logger")
	if !ok {
		panic("middleware/middleware: cannot extract logger: not present in request context")
	}
	return v.(*log.Entry)
}

func ExtractServer(c *gin.Context) *server.Server {
	v, ok := c.Get("server")
	if !ok {
		panic("middleware/middleware: cannot extract server: not present in request context")
	}
	return v.(*server.Server)
}

func ExtractApiClient(c *gin.Context) remote.Client {
	if v, ok := c.Get("api_client"); ok {
		return v.(remote.Client)
	}
	panic("middleware/middlware: cannot extract api clinet: not present in context")
}

func ExtractManager(c *gin.Context) *server.Manager {
	if v, ok := c.Get("manager"); ok {
		return v.(*server.Manager)
	}
	panic("middleware/middleware: cannot extract server manager: not present in context")
}