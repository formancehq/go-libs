package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponseWriter_Write(t *testing.T) {
	t.Run("normal content", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rw := &responseWriter{
			ResponseWriter: rec,
			data:           []byte{},
		}

		n, err := rw.Write([]byte("test data"))
		require.NoError(t, err, "L'écriture ne devrait pas échouer")
		require.Equal(t, 9, n, "Le nombre d'octets écrits devrait être correct")
		require.Equal(t, []byte("test data"), rw.data, "Les données devraient être stockées")
		require.Empty(t, rec.Body.String(), "Rien ne devrait être écrit sur le ResponseWriter sous-jacent")
	})

	t.Run("binary content", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "application/octet-stream")
		rw := &responseWriter{
			ResponseWriter: rec,
			data:           []byte{},
		}

		n, err := rw.Write([]byte("binary data"))
		require.NoError(t, err, "L'écriture ne devrait pas échouer")
		require.Equal(t, 11, n, "Le nombre d'octets écrits devrait être correct")
		require.Empty(t, rw.data, "Les données ne devraient pas être stockées")
		require.Equal(t, "binary data", rec.Body.String(), "Les données devraient être écrites directement")
	})
}

func TestResponseWriter_Finalize(t *testing.T) {
	t.Run("normal content", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rw := &responseWriter{
			ResponseWriter: rec,
			data:           []byte("test data"),
		}

		rw.finalize()
		require.Equal(t, "test data", rec.Body.String(), "Les données devraient être écrites sur le ResponseWriter sous-jacent")
	})

	t.Run("binary content", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "application/octet-stream")
		rw := &responseWriter{
			ResponseWriter: rec,
			data:           []byte("test data"),
		}

		rec.Write([]byte("binary data"))

		rw.finalize()
		require.Equal(t, "binary data", rec.Body.String(), "Les données ne devraient pas être modifiées")
	})
}

func TestOTLPMiddleware(t *testing.T) {
	t.Run("without debug", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("success"))
		})

		middleware := OTLPMiddleware("test-server", false)
		wrappedHandler := middleware(handler)

		req := httptest.NewRequest("GET", "http://example.com/foo", nil)
		rec := httptest.NewRecorder()

		wrappedHandler.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code, "Le code de statut devrait être 200")
		require.Equal(t, "success", rec.Body.String(), "Le corps de la réponse devrait être correct")
	})

	t.Run("with debug", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("success"))
		})

		middleware := OTLPMiddleware("test-server", true)
		wrappedHandler := middleware(handler)

		req := httptest.NewRequest("GET", "http://example.com/foo", strings.NewReader("request body"))
		req.Header.Set("Content-Type", "text/plain")
		rec := httptest.NewRecorder()

		wrappedHandler.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code, "Le code de statut devrait être 200")
		require.Equal(t, "success", rec.Body.String(), "Le corps de la réponse devrait être correct")
	})

	t.Run("with debug and binary response", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte{0x01, 0x02, 0x03})
		})

		middleware := OTLPMiddleware("test-server", true)
		wrappedHandler := middleware(handler)

		req := httptest.NewRequest("GET", "http://example.com/foo", nil)
		rec := httptest.NewRecorder()

		wrappedHandler.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code, "Le code de statut devrait être 200")
		require.Equal(t, []byte{0x01, 0x02, 0x03}, rec.Body.Bytes(), "Le corps de la réponse devrait être correct")
	})
}

type failingResponseWriter struct {
	http.ResponseWriter
}

func (w *failingResponseWriter) Write(data []byte) (int, error) {
	return 0, io.ErrClosedPipe
}

func TestResponseWriter_Finalize_Error(t *testing.T) {
	defer func() {
		r := recover()
		require.NotNil(t, r, "La fonction devrait paniquer")
		err, ok := r.(error)
		require.True(t, ok, "La panique devrait être une erreur")
		require.Equal(t, io.ErrClosedPipe, err, "L'erreur devrait être ErrClosedPipe")
	}()

	rw := &responseWriter{
		ResponseWriter: &failingResponseWriter{httptest.NewRecorder()},
		data:           []byte("test data"),
	}

	rw.finalize() // Cela devrait paniquer
}
