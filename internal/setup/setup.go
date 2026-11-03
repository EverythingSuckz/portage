package setup

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"sync"
	"time"

	"go.wrench.rocks/portage/internal/simplefin"
)

const timeout = 10 * time.Minute

//go:embed page.html
var pageHTML string

var page = template.Must(template.New("page").Parse(pageHTML))

type Page struct {
	ID   string
	URL  string
	Done chan error // nil on success

	dataDir string
	srv     *http.Server
	once    sync.Once
}

func Start(dataDir, bank string) (*Page, error) {
	if bank != "simplefin" {
		return nil, fmt.Errorf("bank %q not supported", bank)
	}
	id := rand.Text()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	p := &Page{
		ID:      id,
		URL:     fmt.Sprintf("http://127.0.0.1:%d/%s/", port, id),
		Done:    make(chan error, 1),
		dataDir: dataDir,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{id}/", p.form)
	mux.HandleFunc("POST /{id}/simplefin", p.simplefin)
	p.srv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go p.srv.Serve(ln)
	time.AfterFunc(timeout, func() { p.finish(errors.New("setup timed out")) })
	return p, nil
}

func (p *Page) finish(err error) {
	p.once.Do(func() {
		go func() {
			p.srv.Shutdown(context.Background())
			p.Done <- err
		}()
	})
}

func (p *Page) validID(r *http.Request) bool {
	return subtle.ConstantTimeCompare([]byte(r.PathValue("id")), []byte(p.ID)) == 1
}

func (p *Page) form(w http.ResponseWriter, r *http.Request) {
	if !p.validID(r) {
		http.NotFound(w, r)
		return
	}
	page.Execute(w, view{Step: "simplefin", ID: p.ID})
}

func (p *Page) simplefin(w http.ResponseWriter, r *http.Request) {
	if !p.validID(r) {
		http.NotFound(w, r)
		return
	}
	accessURL, err := simplefin.Claim(r.Context(), r.FormValue("token"))
	if err != nil {
		page.Execute(w, view{Step: "simplefin", ID: p.ID, Error: err.Error()})
		return
	}
	err = simplefin.Save(p.dataDir, accessURL)
	if err != nil {
		page.Execute(w, view{Step: "simplefin", ID: p.ID, Error: err.Error()})
		return
	}
	page.Execute(w, view{Step: "done", ID: p.ID})
	p.finish(nil)
}

type view struct {
	Step  string
	ID    string
	Error string
}
