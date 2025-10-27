package api

import (
	"net/http"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/hlog"
)

func addMiddleware(logger zerolog.Logger, next http.Handler) http.Handler {
	for _, m := range []func(http.Handler) http.Handler{
		hlog.AccessHandler(func(r *http.Request, status, size int, duration time.Duration) {
			log := zerolog.Ctx(r.Context())

			var evt *zerolog.Event
			switch {
			case status >= 400 && status <= 599:
				evt = log.Error()
			case r.URL.Path == "/v1/version":
				// The version endpoint is used for health checks
				evt = log.Debug()
			default:
				evt = log.Info()
			}

			// DataDog wants nanoseconds in the duration field. The Separate ms
			// duration is for humans.
			evt.Int("http.status_code", status).
				Int64("duration_ms", duration.Milliseconds()).
				Int64("duration", duration.Nanoseconds()).
				Int("http.response.content_length", size).
				Msg("http request")
		}),
		hlog.URLHandler("http.url"),
		hlog.MethodHandler("http.method"),
		hlog.RemoteIPHandler("http.client_ip"),
		hlog.UserAgentHandler("http.useragent"),
		hlog.NewHandler(logger),
	} {
		next = m(next)
	}

	return next
}
