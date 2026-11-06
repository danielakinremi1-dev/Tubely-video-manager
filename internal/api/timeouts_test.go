package api

import (
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// dribble returns a reader that delivers data in n chunks with a pause
// before each, like a client on a very slow connection.
func dribble(data []byte, n int, pause time.Duration) io.Reader {
	pr, pw := io.Pipe()
	go func() {
		size := (len(data) + n - 1) / n
		for len(data) > 0 {
			time.Sleep(pause)
			k := min(size, len(data))
			if _, err := pw.Write(data[:k]); err != nil {
				return
			}
			data = data[k:]
		}
		pw.Close()
	}()
	return pr
}

// The server-wide read timeout stops slow clients, but authenticated upload
// routes extend their own deadline.
func TestUploadRoutesOutliveServerReadTimeout(t *testing.T) {
	srv := newTestServer(t)
	owner, token := newTestUser(t, srv, "owner@example.com")
	video := newTestVideo(t, srv, owner)

	hs := srv.HTTPServer("")
	hs.ReadTimeout = 300 * time.Millisecond
	hs.WriteTimeout = 300 * time.Millisecond
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go hs.Serve(ln)
	t.Cleanup(func() { hs.Close() })
	base := "http://" + ln.Addr().String()

	// ~1s to send, over three times the read timeout.
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	body, ctype := multipartBody(t, "thumbnail", "image/png", png)
	req, _ := http.NewRequest("POST", base+"/api/thumbnail_upload/"+video.ID.String(), dribble(body.Bytes(), 10, 100*time.Millisecond))
	req.Header.Set("Content-Type", ctype)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("slow thumbnail upload: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("slow thumbnail upload: status %d, want 200", res.StatusCode)
	}

	// The same pace on an ordinary route hits the server-wide timeout.
	slowJSON := []byte(`{"title": "slow", "description": "` + strings.Repeat("x", 100) + `"}`)
	req, _ = http.NewRequest("POST", base+"/api/videos", dribble(slowJSON, 10, 100*time.Millisecond))
	req.Header.Set("Authorization", "Bearer "+token)
	res, err = http.DefaultClient.Do(req)
	if err == nil {
		res.Body.Close()
		if res.StatusCode == http.StatusCreated {
			t.Error("slow JSON request succeeded, want the read timeout to stop it")
		}
	}
}
