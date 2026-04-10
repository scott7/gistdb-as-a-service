package main

import (
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
	"log"
	"math/big"
	"net"
	"net/http"
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
	fmt.Printf("Password for %s: ", username)
	raw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
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
			fmt.Printf("Updated password for %s in %s\n", username, credsPath)
			return
		}
	}

	existing = append(existing, credential{Username: username, Hash: string(hash)})
	if err := saveCredentials(credsPath, existing); err != nil {
		log.Fatalf("saving credentials: %v", err)
	}
	fmt.Printf("Added user %s to %s\n", username, credsPath)
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

func generateSelfSignedCert(extraIPs []net.IP) (tls.Certificate, error) {
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
	ips = append(ips, extraIPs...)

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

// Login page
// Injecting login page here to prevent the admin page (indenx.html) from being exposed prior to auth.

const loginPageTmpl = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>GistDB Admin — Sign in</title>
<link rel="icon" href="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 32 32'><rect width='32' height='32' rx='6' fill='%231a1a1a'/><path d='M7 11v10c0 2.2 4 4 9 4s9-1.8 9-4V11' fill='%232563eb'/><ellipse cx='16' cy='11' rx='9' ry='3.5' fill='%233b82f6'/><ellipse cx='16' cy='17' rx='9' ry='3.5' fill='none' stroke='%2393c5fd' stroke-width='1.2'/><ellipse cx='16' cy='21' rx='9' ry='3.5' fill='%231d4ed8'/></svg>">
<style>
  *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
    background: #f5f5f5;
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100vh;
  }
  .card {
    background: #fff;
    border-radius: 8px;
    box-shadow: 0 4px 24px rgba(0,0,0,0.1);
    padding: 36px 40px;
    width: 360px;
  }
  h1 { font-size: 20px; font-weight: 700; margin-bottom: 6px; }
  .subtitle { font-size: 13px; color: #6b7280; margin-bottom: 28px; }
  .field { display: flex; flex-direction: column; gap: 5px; margin-bottom: 16px; }
  label { font-size: 12px; font-weight: 600; color: #374151; }
  input {
    border: 1px solid #d1d5db;
    border-radius: 5px;
    padding: 8px 10px;
    font-size: 14px;
    height: 36px;
  }
  input:focus { outline: none; border-color: #3b82f6; }
  button {
    width: 100%;
    background: #1a1a1a;
    color: #fff;
    border: none;
    border-radius: 5px;
    padding: 9px;
    font-size: 14px;
    font-weight: 500;
    cursor: pointer;
    margin-top: 8px;
    height: 38px;
  }
  button:hover { background: #333; }
  .error {
    background: #fef2f2;
    border: 1px solid #fecaca;
    border-radius: 5px;
    color: #dc2626;
    font-size: 13px;
    padding: 9px 12px;
    margin-bottom: 16px;
  }
</style>
</head>
<body>
<div class="card">
  <h1>GistDB Admin</h1>
  <p class="subtitle">Sign in to continue</p>
  {{if .Error}}<div class="error">{{.Error}}</div>{{end}}
  <form method="POST" action="/login" autocomplete="off">
    <div class="field">
      <label for="username">Username</label>
      <input id="username" name="username" type="text" autofocus autocomplete="off">
    </div>
    <div class="field">
      <label for="password">Password</label>
      <input id="password" name="password" type="password" autocomplete="off">
    </div>
    <button type="submit">Sign in</button>
  </form>
</div>
</body>
</html>`

var loginTmpl = template.Must(template.New("login").Parse(loginPageTmpl))

func loginHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		loginTmpl.Execute(w, map[string]string{"Error": ""})

	case http.MethodPost:
		r.ParseForm()
		username := r.FormValue("username")
		password := r.FormValue("password")

		if !validateCredentials(username, password) {
			w.WriteHeader(http.StatusUnauthorized)
			loginTmpl.Execute(w, map[string]string{"Error": "Invalid username or password"})
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

// Token handler

var privateKey *rsa.PrivateKey

func tokenHandler(w http.ResponseWriter, r *http.Request) {
	if privateKey == nil {
		http.Error(w, "private key not loaded", http.StatusServiceUnavailable)
		return
	}
	claims := jwt.MapClaims{
		"iss": "node-api",
		"aud": "go-db-service",
		"exp": time.Now().Add(6 * time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := token.SignedString(privateKey)
	if err != nil {
		http.Error(w, "failed to sign token", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"token": signed})
}

func main() {
	port := flag.String("port", "8081", "port to serve on")
	keyPath := flag.String("key", "../private.pem", "path to RSA private key PEM file")
	credsPath := flag.String("credentials", "../credentials.json", "path to JSON credentials file")
	newUser := flag.String("add-user", "", "add or update a user in the credentials file and exit")
	flag.Parse()

	serviceURL := os.Getenv("GISTDB_SERVICE_URL")
	if serviceURL == "" {
		serviceURL = "http://localhost:8085"
	}

	if *newUser != "" {
		addUser(*credsPath, *newUser)
		return
	}

	if err := loadCredentials(*credsPath); err != nil {
		log.Fatalf("credentials: %v\nRun with --add-user <username> to create credentials.", err)
	}
	log.Printf("Loaded %d user(s) from %s", len(credentials), *credsPath)

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

	mux := http.NewServeMux()
	mux.HandleFunc("/login", loginHandler)
	mux.HandleFunc("/logout", logoutHandler)
	mux.Handle("/token", requireSession(http.HandlerFunc(tokenHandler)))
	blocked := map[string]bool{
		"/main.go":   true,
		"/README.md": true,
		"/readme.md": true,
	}
	indexTmpl := template.Must(template.ParseFiles("index.html"))
	fileServer := http.FileServer(http.Dir("."))
	mux.Handle("/", requireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if blocked[r.URL.Path] {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			indexTmpl.Execute(w, map[string]string{"ServiceURL": serviceURL})
			return
		}
		fileServer.ServeHTTP(w, r)
	})))

	var extraIPs []net.IP
	for _, s := range strings.Split(os.Getenv("CERT_IPS"), ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if ip := net.ParseIP(s); ip != nil {
			extraIPs = append(extraIPs, ip)
		} else {
			log.Printf("warning: CERT_IPS contains invalid IP %q, skipping", s)
		}
	}
	if len(extraIPs) > 0 {
		log.Printf("Adding %d extra IP(s) to TLS cert SAN from CERT_IPS", len(extraIPs))
	}

	cert, err := generateSelfSignedCert(extraIPs)
	if err != nil {
		log.Fatalf("generating TLS cert: %v", err)
	}
	tlsConfig := &tls.Config{Certificates: []tls.Certificate{cert}}

	ln, err := tls.Listen("tcp", ":"+*port, tlsConfig)
	if err != nil {
		log.Fatalf("listening: %v", err)
	}

	log.Printf("Client serving on https://localhost:%s (self-signed cert)", *port)
	log.Fatal(http.Serve(ln, mux))
}
