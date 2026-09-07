package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

// Credentials

type credential struct {
	Username string `json:"username"`
	Hash     string `json:"hash"`
}

var credentials []credential

func loadCredentials(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading credentials file: %w", err)
	}
	return json.Unmarshal(data, &credentials)
}

func saveCredentials(path string, creds []credential) error {
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func addUser(credsPath, username string) {
	log.Printf("Password for %s: ", username)
	raw, err := term.ReadPassword(int(os.Stdin.Fd()))
	log.Println()
	if err != nil {
		log.Fatalf("reading password: %v", err)
	}
	password := string(raw)
	if password == "" {
		log.Fatal("password cannot be empty")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		log.Fatalf("bcrypt error: %v", err)
	}

	var existing []credential
	if data, err := os.ReadFile(credsPath); err == nil {
		json.Unmarshal(data, &existing)
	}

	for i, c := range existing {
		if c.Username == username {
			existing[i].Hash = string(hash)
			if err := saveCredentials(credsPath, existing); err != nil {
				log.Fatalf("saving credentials: %v", err)
			}
			log.Printf("Updated password for %s in %s\n", username, credsPath)
			return
		}
	}

	existing = append(existing, credential{Username: username, Hash: string(hash)})
	if err := saveCredentials(credsPath, existing); err != nil {
		log.Fatalf("saving credentials: %v", err)
	}
	log.Printf("Added user %s to %s\n", username, credsPath)
}

func validateCredentials(username, password string) bool {
	for _, c := range credentials {
		if c.Username == username {
			return bcrypt.CompareHashAndPassword([]byte(c.Hash), []byte(password)) == nil
		}
	}
	return false
}

// Sessions

type sessionEntry struct {
	username  string
	expiresAt time.Time
}

var (
	sessions   = map[string]sessionEntry{}
	sessionsMu sync.RWMutex
	sessionTTL = 24 * time.Hour
)

func newSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

func validSession(id string) bool {
	sessionsMu.RLock()
	entry, ok := sessions[id]
	sessionsMu.RUnlock()
	return ok && time.Now().Before(entry.expiresAt)
}

func createSession(username string) (string, error) {
	id, err := newSessionID()
	if err != nil {
		return "", err
	}
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	// sweep expired sessions
	for k, v := range sessions {
		if time.Now().After(v.expiresAt) {
			delete(sessions, k)
		}
	}
	sessions[id] = sessionEntry{username: username, expiresAt: time.Now().Add(sessionTTL)}
	return id, nil
}

func deleteSession(id string) {
	sessionsMu.Lock()
	delete(sessions, id)
	sessionsMu.Unlock()
}

func sessionCookie(id string) *http.Cookie {
	return &http.Cookie{
		Name:     "session",
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	}
}

// TLS

func loadRealCert(certFile, keyFile string) (tls.Certificate, error) {
	return tls.LoadX509KeyPair(certFile, keyFile)
}

func generateSelfSignedCert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}

	ips := []net.IP{net.ParseIP("127.0.0.1")}
	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			addrs, _ := iface.Addrs()
			for _, addr := range addrs {
				if ipNet, ok := addr.(*net.IPNet); ok {
					if ip4 := ipNet.IP.To4(); ip4 != nil && !ip4.IsLoopback() {
						ips = append(ips, ip4)
					}
				}
			}
		}
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{Organization: []string{"GistDB Admin"}},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  ips,
		DNSNames:     []string{"localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return tls.X509KeyPair(certPEM, keyPEM)
}

// Auth middleware

func requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err != nil || !validSession(cookie.Value) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Template rendering

// renderTemplate buffers the render so a template failure is loud: escaping
// errors surface only at execute time, and writing straight to the
// ResponseWriter turns them into a blank 200 with a half-written body.
func renderTemplate(w http.ResponseWriter, name string, t *template.Template, status int, data any) {
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		log.Printf("ERROR: rendering %s template: %v", name, err)
		http.Error(w, "internal error: page template failed to render", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if _, err := buf.WriteTo(w); err != nil {
		log.Printf("ERROR: writing %s response: %v", name, err)
	}
}

// checkTemplate renders once at startup with representative data so an
// escaping error fails the process immediately instead of on first request.
func checkTemplate(name string, t *template.Template, data any) {
	if err := t.Execute(io.Discard, data); err != nil {
		log.Fatalf("ERROR: %s template is invalid: %v", name, err)
	}
}

// Login page
// Injecting login page here to prevent the admin page (indenx.html) from being exposed prior to auth.

const loginPageTmpl = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>GistDB Admin — Sign in</title>
<link rel="icon" href="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 32 32'><rect width='32' height='32' rx='6' fill='%231a1a1a'/><path d='M7 11v10c0 2.2 4 4 9 4s9-1.8 9-4V11' fill='%232563eb'/><ellipse cx='16' cy='11' rx='9' ry='3.5' fill='%233b82f6'/><ellipse cx='16' cy='17' rx='9' ry='3.5' fill='none' stroke='%2393c5fd' stroke-width='1.2'/><ellipse cx='16' cy='21' rx='9' ry='3.5' fill='%231d4ed8'/></svg>">
<script>
  // Same preference the admin page writes, applied before first paint so the
  // two screens agree. No stored value means auto (prefers-color-scheme).
  (function () {
    var t = localStorage.getItem('theme');
    if (t) document.documentElement.setAttribute('data-theme', t);
  })();
</script>
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/@picocss/pico@2/css/pico.min.css">
<style>
  /* Pico supplies the card, form controls and palette; this is just centring. */
  :root {
    --pico-font-size: 87.5%;
    --pico-border-radius: 0.25rem;
  }

  body {
    display: grid;
    place-items: center;
    min-height: 100dvh;
    margin: 0;
    padding: 1rem;
  }

  main { width: 22rem; max-width: 100%; }

  article { margin: 0; padding: 1.5rem 1.75rem; }

  h1 { font-size: 1.15rem; margin-bottom: 0.15rem; }

  .subtitle {
    color: var(--pico-muted-color);
    font-size: 0.85rem;
    margin-bottom: 1.5rem;
  }

  .error {
    border: 1px solid var(--pico-del-color);
    border-radius: var(--pico-border-radius);
    color: var(--pico-del-color);
    font-size: 0.85rem;
    padding: 0.6rem 0.75rem;
    margin-bottom: 1.25rem;
  }
</style>
</head>
<body>
<main>
  <article>
    <h1>GistDB Admin</h1>
    <p class="subtitle">Sign in to continue</p>
    {{if .Error}}<p class="error">{{.Error}}</p>{{end}}
    <form method="POST" action="/login" autocomplete="off">
      <label for="username">Username</label>
      <input id="username" name="username" type="text" autofocus autocomplete="off"{{if .Error}} aria-invalid="true"{{end}}>

      <label for="password">Password</label>
      <input id="password" name="password" type="password" autocomplete="off"{{if .Error}} aria-invalid="true"{{end}}>

      <button type="submit">Sign in</button>
    </form>
  </article>
</main>
</body>
</html>`

var loginTmpl = template.Must(template.New("login").Parse(loginPageTmpl))

func loginHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		renderTemplate(w, "login", loginTmpl, http.StatusOK, map[string]string{"Error": ""})

	case http.MethodPost:
		r.ParseForm()
		username := r.FormValue("username")
		password := r.FormValue("password")

		if !validateCredentials(username, password) {
			renderTemplate(w, "login", loginTmpl, http.StatusUnauthorized, map[string]string{"Error": "Invalid username or password"})
			return
		}

		id, err := createSession(username)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		http.SetCookie(w, sessionCookie(id))
		http.Redirect(w, r, "/", http.StatusSeeOther)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("session"); err == nil {
		deleteSession(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:   "session",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// JWT and proxy

var (
	privateKey *rsa.PrivateKey
	demoMode   bool
)

func generateJWT() (string, error) {
	claims := jwt.MapClaims{
		"iss": "node-api",
		"aud": "go-db-service",
		"exp": time.Now().Add(2 * time.Minute).Unix(),
		"iat": time.Now().Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(privateKey)
}

func makeAPIProxy(target *url.URL) http.Handler {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.URL.Path = strings.TrimPrefix(pr.In.URL.Path, "/api")
			if pr.Out.URL.Path == "" {
				pr.Out.URL.Path = "/"
			}
			pr.Out.URL.RawQuery = pr.In.URL.RawQuery
			pr.Out.Host = target.Host
			if signed, err := generateJWT(); err == nil {
				pr.Out.Header.Set("Authorization", "Bearer "+signed)
			}
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("proxy error: %v", err)
			http.Error(w, "upstream error", http.StatusBadGateway)
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if privateKey == nil {
			http.Error(w, "private key not loaded", http.StatusServiceUnavailable)
			return
		}
		if demoMode && r.Method != http.MethodGet {
			http.Error(w, "read-only in demo mode", http.StatusMethodNotAllowed)
			return
		}
		proxy.ServeHTTP(w, r)
	})
}

func main() {
	port := flag.String("port", "8081", "port to serve on")
	keyPath := flag.String("key", "../private.pem", "path to RSA private key PEM file")
	credsPath := flag.String("credentials", "../credentials.json", "path to JSON credentials file")
	newUser := flag.String("add-user", "", "add or update a user in the credentials file and exit")
	flag.Parse()

	rawServiceURL := os.Getenv("GISTDB_SERVICE_URL")
	if rawServiceURL == "" {
		rawServiceURL = "http://localhost:8085"
	}
	serviceURL, urlErr := url.Parse(rawServiceURL)
	if urlErr != nil {
		log.Fatalf("invalid GISTDB_SERVICE_URL: %v", urlErr)
	}

	demoMode = os.Getenv("DEMO_MODE") != ""
	if demoMode {
		log.Println("Demo mode enabled: login skipped, writes blocked")
	}

	if *newUser != "" {
		addUser(*credsPath, *newUser)
		return
	}

	if !demoMode {
		if err := loadCredentials(*credsPath); err != nil {
			log.Fatalf("credentials: %v\nRun with --add-user <username> to create credentials.", err)
		}
		log.Printf("Loaded %d user(s) from %s", len(credentials), *credsPath)
	}

	var keyData []byte
	if pemEnv := os.Getenv("JWT_PRIVATE_KEY"); pemEnv != "" {
		var decErr error
		keyData, decErr = base64.StdEncoding.DecodeString(pemEnv)
		if decErr != nil {
			log.Fatalf("decoding JWT_PRIVATE_KEY: %v", decErr)
		}
	} else if data, err := os.ReadFile(*keyPath); err != nil {
		log.Printf("warning: could not read private key at %s: %v", *keyPath, err)
	} else {
		keyData = data
	}
	if len(keyData) > 0 {
		if pk, err := jwt.ParseRSAPrivateKeyFromPEM(keyData); err != nil {
			log.Printf("warning: could not parse private key: %v", err)
		} else {
			privateKey = pk
		}
	}

	apiProxy := makeAPIProxy(serviceURL)

	mux := http.NewServeMux()
	if demoMode {
		mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/", http.StatusFound)
		})
	} else {
		mux.HandleFunc("/login", loginHandler)
	}
	mux.HandleFunc("/logout", logoutHandler)
	if demoMode {
		mux.Handle("/api/", apiProxy)
	} else {
		mux.Handle("/api/", requireSession(apiProxy))
	}

	blocked := map[string]bool{
		"/main.go":   true,
		"/README.md": true,
		"/readme.md": true,
	}
	indexTmpl := template.Must(template.ParseFiles("index.html"))
	checkTemplate("index", indexTmpl, map[string]any{"DemoMode": demoMode})
	checkTemplate("login", loginTmpl, map[string]string{"Error": "check"})
	fileServer := http.FileServer(http.Dir("."))
	indexH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if blocked[r.URL.Path] {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			renderTemplate(w, "index", indexTmpl, http.StatusOK, map[string]any{"DemoMode": demoMode})
			return
		}
		fileServer.ServeHTTP(w, r)
	})
	if demoMode {
		mux.Handle("/", indexH)
	} else {
		mux.Handle("/", requireSession(indexH))
	}

	var (
		ln  net.Listener
		err error
	)
	if os.Getenv("NO_TLS") != "" {
		ln, err = net.Listen("tcp", ":"+*port)
		if err != nil {
			log.Fatalf("listening: %v", err)
		}
		log.Printf("Client serving on http://localhost:%s (no TLS)", *port)
	} else {
		var cert tls.Certificate
		certFile, keyFile := os.Getenv("TLS_CERT_FILE"), os.Getenv("TLS_KEY_FILE")
		if certFile != "" && keyFile != "" {
			cert, err = loadRealCert(certFile, keyFile)
			if err != nil {
				log.Fatalf("loading TLS cert from %s / %s: %v", certFile, keyFile, err)
			}
			log.Printf("Client serving on https://localhost:%s (real cert)", *port)
		} else {
			cert, err = generateSelfSignedCert()
			if err != nil {
				log.Fatalf("generating TLS cert: %v", err)
			}
			log.Printf("Client serving on https://localhost:%s (self-signed cert)", *port)
		}
		ln, err = tls.Listen("tcp", ":"+*port, &tls.Config{Certificates: []tls.Certificate{cert}})
		if err != nil {
			log.Fatalf("listening: %v", err)
		}
	}
	log.Fatal(http.Serve(ln, mux))
}
