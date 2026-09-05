package swaputil

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"
)

// TestReplaceRequestModel_MultipartPreservesFilePartHeaders verifies that model
// rewriting is transparent to uploaded files. In particular, per-part MIME and
// application-specific headers must survive the multipart reconstruction.
func TestReplaceRequestModel_MultipartPreservesFilePartHeaders(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", "public"); err != nil {
		t.Fatalf("write model: %v", err)
	}

	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="audio.wav"`)
	header.Set("Content-Type", "audio/wav")
	header.Set("X-Part-ID", "part-123")
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatalf("create file part: %v", err)
	}
	if _, err := part.Write([]byte("RIFFdata")); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}

	r := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", bytes.NewReader(body.Bytes()))
	r.Header.Set("Content-Type", writer.FormDataContentType())

	updated, err := ReplaceRequestModel(r, "public", "target")
	if err != nil {
		t.Fatalf("ReplaceRequestModel: %v", err)
	}
	rewritten, err := io.ReadAll(updated.Body)
	if err != nil {
		t.Fatalf("read rewritten body: %v", err)
	}
	updated.Body = io.NopCloser(bytes.NewReader(rewritten))
	if err := updated.ParseMultipartForm(MaxMultiPartSize); err != nil {
		t.Fatalf("parse rewritten multipart: %v", err)
	}

	files := updated.MultipartForm.File["file"]
	if len(files) != 1 {
		t.Fatalf("file part count = %d, want 1", len(files))
	}
	fh := files[0]
	if got := fh.Header.Get("Content-Type"); got != "audio/wav" {
		t.Fatalf("file Content-Type after model rewrite = %q, want %q", got, "audio/wav")
	}
	if got := fh.Header.Get("X-Part-ID"); got != "part-123" {
		t.Fatalf("file X-Part-ID after model rewrite = %q, want %q", got, "part-123")
	}
}
