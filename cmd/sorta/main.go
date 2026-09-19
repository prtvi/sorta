package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/prtvi/sorta/internal/filesystem"
	"github.com/prtvi/sorta/internal/server"
)

// Placeholder keeps the embed valid before the React build exists.
// Production builds copy web/dist into this directory.
//
//go:embed all:webdist
var webDist embed.FS

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address (localhost only by default)")
	noOpen := flag.Bool("no-open", false, "Do not open the browser automatically")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: sorta [flags] <photo-directory>\n\n")
		fmt.Fprintf(os.Stderr, "Local-first photo culling. Classifies photos into liked/, disliked/, review/.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}

	root := flag.Arg(0)
	store, err := filesystem.NewStore(root)
	if err != nil {
		log.Fatalf("invalid directory: %v", err)
	}
	if err := store.EnsureClassificationDirs(); err != nil {
		log.Fatalf("create classification dirs: %v", err)
	}

	api := server.New(store).Handler()
	handler := http.Handler(api)

	if distFS, err := fs.Sub(webDist, "webdist"); err == nil {
		if hasIndex(distFS) {
			handler = spaHandler(distFS, api)
			log.Printf("Serving embedded frontend")
		} else {
			log.Printf("No embedded frontend (API only). Run Vite in development or build web/ first.")
		}
	}

	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	url := fmt.Sprintf("http://%s", listener.Addr().String())
	log.Printf("Sorta ready")
	log.Printf("Working directory: %s", store.Root)
	log.Printf("Listening on %s", url)

	if !*noOpen {
		go func() {
			time.Sleep(200 * time.Millisecond)
			_ = openBrowser(url)
		}()
	}

	if err := http.Serve(listener, handler); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func hasIndex(dist fs.FS) bool {
	f, err := dist.Open("index.html")
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

func spaHandler(dist fs.FS, api http.Handler) http.Handler {
	static := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/photos/") {
			api.ServeHTTP(w, r)
			return
		}
		name := strings.TrimPrefix(path, "/")
		if name == "" {
			name = "index.html"
		}
		if f, err := dist.Open(name); err == nil {
			_ = f.Close()
			static.ServeHTTP(w, r)
			return
		}
		// SPA fallback
		r.URL.Path = "/"
		static.ServeHTTP(w, r)
	})
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
