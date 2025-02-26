package api

import (
	"net/http"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/hlog"
)

func addMiddleware(logger zerolog.Logger, next http.Handler) http.Handler {
	for _, m := range []func(http.Handler) http.Handler{
		hlog.NewHandler(logger),
		hlog.URLHandler("http.url"),
		hlog.MethodHandler("http.method"),
		hlog.RemoteIPHandler("http.client_ip"),
		hlog.UserAgentHandler("http.useragent"),
		hlog.AccessHandler(func(r *http.Request, status, size int, duration time.Duration) {
			log := zerolog.Ctx(r.Context())

			var evt *zerolog.Event
			if status >= 400 && status <= 599 {
				evt = log.Error()
			} else {
				evt = log.Debug()
			}

			// DataDog wants nanoseconds in the duration field. The Separate ms
			// duration is for humans.
			evt.Int("http.status_code", status).
				Int64("duration_ms", duration.Milliseconds()).
				Int64("duration", duration.Nanoseconds()).
				Int("http.response.content_length", size).
				Msg("http request")
		}),
	} {
		next = m(next)
	}

	return next
}
