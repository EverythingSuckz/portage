package setup

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"go.wrench.rocks/portage/internal/rogers"
	"go.wrench.rocks/portage/internal/simplefin"
)

// timeout is how long a setup page stays open. Tests shorten it.
var timeout = 10 * time.Minute

// claim exchanges a SimpleFIN setup token for an access URL.
var claim = simplefin.Claim

//go:embed page.html
var pageHTML string

// page renders every step of the setup flow.
var page = template.Must(template.New("page").Parse(pageHTML))

type Page struct {
	ID   string
	URL  string
	Done chan error // receives one result: nil on success

	dataDir  string
	srv      *http.Server
	once     sync.Once
	bank     string
	ctx      context.Context // cancelled by finish, which wakes any waiting goroutines
	cancel   context.CancelFunc
	prompter *webPrompter
	running  atomic.Bool // a Rogers login is in progress
}

// starts a setup page for the given bank.
func Start(dataDir, bank string) (*Page, error) {
	if bank != "simplefin" && bank != "rogers" {
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
		bank:    bank,
		dataDir: dataDir,
	}

	p.ctx, p.cancel = context.WithCancel(context.Background())

	p.prompter = &webPrompter{
		ctx:       p.ctx,
		questions: make(chan question),
		answers:   make(chan string),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{id}/", p.form)
	mux.HandleFunc("POST /{id}/simplefin", p.simplefin)
	mux.HandleFunc("POST /{id}/rogers", p.rogers)
	mux.HandleFunc("POST /{id}/answer", p.answer)
	p.srv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go p.srv.Serve(ln)
	time.AfterFunc(timeout, func() { p.finish(errors.New("setup timed out")) })
	return p, nil
}

// finish reports err on Done and shuts the page down.
func (p *Page) finish(err error) {
	p.once.Do(func() {
		p.cancel()
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
	p.render(w, view{Step: p.bank, ID: p.ID})
}

func (p *Page) simplefin(w http.ResponseWriter, r *http.Request) {
	if !p.validID(r) {
		http.NotFound(w, r)
		return
	}
	accessURL, err := claim(r.Context(), r.FormValue("token"))
	if err != nil {
		p.render(w, view{Step: "simplefin", ID: p.ID, Error: err.Error()})
		return
	}
	err = simplefin.Save(p.dataDir, accessURL)
	if err != nil {
		p.render(w, view{Step: "simplefin", ID: p.ID, Error: err.Error()})
		return
	}
	p.render(w, view{Step: "done", ID: p.ID})
	p.finish(nil)
}

func (p *Page) rogers(w http.ResponseWriter, r *http.Request) {
	if !p.validID(r) {
		http.NotFound(w, r)
		return
	}

	// for potential race condition
	if !p.running.CompareAndSwap(false, true) {
		p.render(w, view{Step: "rogers", ID: p.ID, Error: "Rogers setup already in progress"})
		return
	}

	params := rogers.LoginParams{
		DataDir:       p.dataDir,
		Name:          "rogers",
		Username:      r.FormValue("username"),
		Password:      r.FormValue("password"),
		StorePassword: true,
	}

	go func() {
		err := login(p.ctx, params, p.prompter)
		// report the result without waiting for an answer.
		select {
		case p.prompter.questions <- question{Kind: "done", Err: err}:
		case <-p.ctx.Done():
		}
	}()
	p.next(w)
}

func (p *Page) answer(w http.ResponseWriter, r *http.Request) {
	if !p.validID(r) {
		http.NotFound(w, r)
		return
	}
	select {
	case p.prompter.answers <- r.FormValue("answer"):
	case <-p.ctx.Done():
		http.Error(w, "setup closed", http.StatusGone)
		return
	}
	p.next(w)
}

// next waits for the login's next question and renders it.
func (p *Page) next(w http.ResponseWriter) {
	select {
	case q := <-p.prompter.questions:
		switch q.Kind {
		case "choose":
			p.render(w, view{Step: "choose", ID: p.ID, Prefs: q.Prefs, Note: q.Note})
		case "code":
			p.render(w, view{Step: "code", ID: p.ID, Note: q.Note})
		case "done":
			p.running.Store(false)
			if q.Err != nil {
				// let the user retry, e.g. after a wrong password.
				p.render(w, view{Step: "rogers", ID: p.ID, Error: q.Err.Error()})
				return
			}
			p.render(w, view{Step: "done", ID: p.ID})
			p.finish(nil)
		}
	case <-p.ctx.Done():
		// the page timed out or finished
	}
}

func (p *Page) render(w http.ResponseWriter, v view) {
	if err := page.Execute(w, v); err != nil {
		log.Printf("setup page: %v", err)
	}
}

// view is the data page.html renders.
type view struct {
	Step  string // which form to show
	ID    string
	Prefs []rogers.TwoFactorPreference
	Note  string
	Error string
}
