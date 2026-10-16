// Command pocket-ssh-agent is an SSH agent whose private key lives on your
// phone. Every signature must be approved in the phone's PWA.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/tksugimoto-ai/pocket-ssh-agent/internal/broker"
	"github.com/tksugimoto-ai/pocket-ssh-agent/internal/config"
	"github.com/tksugimoto-ai/pocket-ssh-agent/internal/httpapi"
	"github.com/tksugimoto-ai/pocket-ssh-agent/internal/listen"
	"github.com/tksugimoto-ai/pocket-ssh-agent/internal/qrterm"
	"github.com/tksugimoto-ai/pocket-ssh-agent/internal/sshagent"
	"github.com/tksugimoto-ai/pocket-ssh-agent/internal/tailscale"
)

var version = "dev"

const defaultWebURL = "https://tksugimoto-ai.github.io/pocket-ssh-agent/"

type options struct {
	listenAddr string
	socket     string
	configPath string
	publicURL  string
	webURL     string
	name       string
	timeout    time.Duration
	pairTTL    time.Duration
	pair       bool
}

func main() {
	log.SetFlags(log.Ltime)
	if len(os.Args) > 1 && os.Args[1] == "pubkey" {
		if err := printPubkey(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}

	var o options
	fs := flag.NewFlagSet("pocket-ssh-agent", flag.ExitOnError)
	fs.StringVar(&o.listenAddr, "listen", "127.0.0.1:7766", "loopback address for the phone API (expose it with `tailscale serve`)")
	fs.StringVar(&o.socket, "socket", listen.DefaultPath(), "SSH agent socket (named pipe on Windows)")
	fs.StringVar(&o.configPath, "config", "", "configuration file (default: user config dir)")
	fs.StringVar(&o.publicURL, "public-url", "", "URL the phone uses to reach this PC (default: https://<MagicDNS name>:<listen port>)")
	fs.StringVar(&o.webURL, "web-url", defaultWebURL, "URL of the phone PWA")
	fs.StringVar(&o.name, "name", "", "name of this PC shown on the phone (default: hostname)")
	fs.DurationVar(&o.timeout, "timeout", 90*time.Second, "how long to wait for approval")
	fs.DurationVar(&o.pairTTL, "pair-ttl", 10*time.Minute, "validity of a pairing QR code")
	fs.BoolVar(&o.pair, "pair", false, "pair with a (new) phone even if already paired")
	showVersion := fs.Bool("version", false, "print version and exit")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage:\n  pocket-ssh-agent [flags]      run the agent\n  pocket-ssh-agent pubkey       print the paired public key\n\nFlags:\n")
		fs.PrintDefaults()
	}
	_ = fs.Parse(os.Args[1:])
	if *showVersion {
		fmt.Println(version)
		return
	}
	if err := run(o); err != nil {
		log.Fatal(err)
	}
}

func openStore(path string) (*config.Store, error) {
	if path == "" {
		p, err := config.DefaultPath()
		if err != nil {
			return nil, err
		}
		path = p
	}
	return config.Open(path)
}

func printPubkey(args []string) error {
	fs := flag.NewFlagSet("pubkey", flag.ExitOnError)
	cfg := fs.String("config", "", "configuration file")
	_ = fs.Parse(args)
	store, err := openStore(*cfg)
	if err != nil {
		return err
	}
	p, _ := store.Pairing()
	if p == nil {
		return errors.New("not paired yet; run pocket-ssh-agent first")
	}
	fmt.Println(p.PublicKey)
	return nil
}

func run(o options) error {
	store, err := openStore(o.configPath)
	if err != nil {
		return err
	}
	web, err := url.Parse(o.webURL)
	if err != nil || web.Scheme != "https" && web.Hostname() != "localhost" {
		return fmt.Errorf("-web-url must be an https URL: %q", o.webURL)
	}
	if o.name == "" {
		o.name, _ = os.Hostname()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := log.Default()
	b := broker.New()
	api := &httpapi.Server{
		Store:         store,
		Broker:        b,
		AllowedOrigin: origin(web),
		PCName:        o.name,
		Log:           logger,
		OnPaired: func(p *config.Pairing) {
			fmt.Printf("\nPaired with %q.\nRegister this public key with GitHub (Settings > SSH and GPG keys) or your servers:\n\n  %s\n\n", p.DeviceName, p.PublicKey)
		},
	}

	hl, err := net.Listen("tcp", o.listenAddr)
	if err != nil {
		return err
	}
	if !isLoopback(hl.Addr()) {
		hl.Close()
		return fmt.Errorf("-listen must be a loopback address, got %s", o.listenAddr)
	}
	srv := &http.Server{Handler: api.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(hl); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("http: %v", err)
			stop()
		}
	}()

	al, err := listen.Listen(o.socket)
	if err != nil {
		return fmt.Errorf("agent socket: %w", err)
	}
	ag := &sshagent.Agent{Store: store, Broker: b, Timeout: o.timeout, Log: logger}
	go serveAgent(al, ag)

	log.Printf("pocket-ssh-agent %s", version)
	log.Printf("SSH agent:  %s", o.socket)
	log.Printf("Phone API:  http://%s", hl.Addr())

	if p, _ := store.Pairing(); p == nil || o.pair {
		if err := startPairing(ctx, api, o, hl.Addr()); err != nil {
			log.Printf("pairing: %v", err)
		}
	} else {
		log.Printf("Paired with %q. Keep the PWA open on the phone to approve requests.", p.DeviceName)
	}

	<-ctx.Done()
	log.Printf("shutting down")
	al.Close()
	sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return srv.Shutdown(sctx)
}

func serveAgent(l net.Listener, ag *sshagent.Agent) {
	for {
		c, err := l.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				log.Printf("agent accept: %v", err)
			}
			return
		}
		go ag.ServeConn(c)
	}
}

// origin returns the browser origin of u, without a default port.
func origin(u *url.URL) string {
	host := u.Hostname()
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if p := u.Port(); p != "" && !(u.Scheme == "https" && p == "443") && !(u.Scheme == "http" && p == "80") {
		host += ":" + p
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(host)
}

func startPairing(ctx context.Context, api *httpapi.Server, o options, addr net.Addr) error {
	public := o.publicURL
	if public == "" {
		name, err := tailscale.DNSName(ctx)
		if err != nil {
			return fmt.Errorf("could not detect the Tailscale name (%v); pass -public-url", err)
		}
		_, port, _ := net.SplitHostPort(addr.String())
		public = "https://" + name + ":" + port
	}
	public = strings.TrimSuffix(public, "/")

	code, err := api.StartPairing(o.pairTTL)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"v": 1, "url": public, "code": code, "name": o.name})
	link := o.webURL + "#pair=" + base64.RawURLEncoding.EncodeToString(payload)

	_, port, _ := net.SplitHostPort(addr.String())
	fmt.Printf("\nMake sure this PC is reachable from the phone over Tailscale, e.g.:\n\n  tailscale serve --bg --https=%s http://%s\n\n", port, addr)
	fmt.Printf("Scan this QR code with the phone (valid for %s):\n\n", o.pairTTL)
	if err := qrterm.Print(os.Stdout, link); err != nil {
		fmt.Printf("(could not render QR code: %v)\n", err)
	}
	fmt.Printf("\nOr open this link on the phone (it contains a one-time secret, do not share it):\n%s\n\n", link)
	return nil
}

func isLoopback(a net.Addr) bool {
	ta, ok := a.(*net.TCPAddr)
	return ok && ta.IP.IsLoopback()
}
