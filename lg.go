package lg

import (
	"context"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	uuid "github.com/satori/go.uuid"
)

var (
	// log the zap logger
	log *zap.Logger
)

type key int

const requestIDKey key = 0

// Rfc3339NanoEncoder to encode time field to RFC3339Nano format.
func Rfc3339NanoEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(t.Format(time.RFC3339Nano))
}

// InitLogger must be first to be called.
func InitLogger(debug bool) {
	var cfg zap.Config

	if debug {
		cfg = zap.NewDevelopmentConfig()
	} else {
		cfg = zap.NewProductionConfig()
		cfg.EncoderConfig.LevelKey = "lvl"
		cfg.EncoderConfig.MessageKey = "msg"
		cfg.EncoderConfig.TimeKey = "timestamp"
		cfg.EncoderConfig.EncodeTime = Rfc3339NanoEncoder
	}

	var err error
	if log, err = cfg.Build(); err != nil {
		panic("Error create logger.")
	}
}

// L to get the request logger.
func L(ctx context.Context) *zap.Logger {
	if ctx == nil {
		return log
	}
	if l, ok := ctx.Value(requestIDKey).(*zap.Logger); ok {
		return l
	}
	return log
}

// LogRequest to log every request.
func LogRequest(next http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		l := log.With(zap.String("requestID", uuid.NewV1().String()))
		ctx := r.Context()
		ctx = context.WithValue(ctx, requestIDKey, l)
		r = r.WithContext(ctx)

		// Start timer
		start := time.Now()

		// wrap the ResponseWriter
		lw := &basicWriter{ResponseWriter: w}

		// Process request
		next.ServeHTTP(lw, r)
		lw.maybeWriteHeader()

		// Stop timer
		end := time.Now()
		latency := end.Sub(start)
		statusCode := lw.Status()

		l.Info("request",
			zap.String("method", r.Method),
			zap.String("url", r.RequestURI),
			zap.Int("code", statusCode),
			zap.String("clientIP", r.RemoteAddr),
			zap.Int("bytes", lw.bytes),
			zap.Int64("duration", int64(latency)/int64(time.Microsecond)),
		)
	}

	return http.HandlerFunc(fn)
}

// Recoverer the recover middware.
func Recoverer(next http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				// stack := stack(3)
				L(r.Context()).Error("panic", zap.Error(err.(error)), zap.Stack("stack"))
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}
		}()

		next.ServeHTTP(w, r)
	}

	return http.HandlerFunc(fn)
}

// writerProxy is a proxy around an http.ResponseWriter that allows you to hook
// into various parts of the response process.
type writerProxy interface {
	http.ResponseWriter
	// Status returns the HTTP status of the request, or 0 if one has not
	// yet been sent.
	Status() int
	// BytesWritten returns the total number of bytes sent to the client.
	BytesWritten() int
	// Tee causes the response body to be written to the given io.Writer in
	// addition to proxying the writes through. Only one io.Writer can be
	// tee'd to at once: setting a second one will overwrite the first.
	// Writes will be sent to the proxy before being written to this
	// io.Writer. It is illegal for the tee'd writer to be modified
	// concurrently with writes.
	Tee(io.Writer)
	// Unwrap returns the original proxied target.
	Unwrap() http.ResponseWriter
}

// basicWriter wraps a http.ResponseWriter that implements the minimal
// http.ResponseWriter interface.
type basicWriter struct {
	http.ResponseWriter
	wroteHeader bool
	code        int
	bytes       int
	tee         io.Writer
}

func (b *basicWriter) WriteHeader(code int) {
	if !b.wroteHeader {
		b.code = code
		b.wroteHeader = true
		b.ResponseWriter.WriteHeader(code)
	}
}

func (b *basicWriter) Write(buf []byte) (int, error) {
	b.WriteHeader(http.StatusOK)
	n, err := b.ResponseWriter.Write(buf)
	if b.tee != nil {
		_, err2 := b.tee.Write(buf[:n])
		// Prefer errors generated by the proxied writer.
		if err == nil {
			err = err2
		}
	}
	b.bytes += n
	return n, err
}

func (b *basicWriter) maybeWriteHeader() {
	if !b.wroteHeader {
		b.WriteHeader(http.StatusOK)
	}
}
func (b *basicWriter) Status() int {
	return b.code
}
func (b *basicWriter) BytesWritten() int {
	return b.bytes
}
func (b *basicWriter) Tee(w io.Writer) {
	b.tee = w
}
func (b *basicWriter) Unwrap() http.ResponseWriter {
	return b.ResponseWriter
}
