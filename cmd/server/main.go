package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"network-switch-visualizer/internal/auth"
	"network-switch-visualizer/internal/db"
	"network-switch-visualizer/internal/handlers"

	"github.com/soheilhy/cmux"
)

const Version = "1.0.0"

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Initialize DB
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./switches.db"
	}
	if err := db.InitDB(dbPath); err != nil {
		log.Fatal(err)
	}
	defer db.DB.Close()

	// Initialize Auth
	auth.InitAuth()

	// Public Routes
	http.Handle("/api/login", securityHeadersMiddleware(handlers.RateLimiterMiddleware(http.HandlerFunc(handlers.HandleLogin))))
	http.Handle("/api/logout", securityHeadersMiddleware(http.HandlerFunc(handlers.HandleLogout)))
	http.Handle("/api/health", securityHeadersMiddleware(http.HandlerFunc(handlers.HandleHealth)))

	// Protected Routes
	http.Handle("/api/me", securityHeadersMiddleware(http.HandlerFunc(handlers.HandleMe)))
	http.Handle("/api/change-password", securityHeadersMiddleware(handlers.AuthMiddleware(handlers.HandleChangePassword)))
	http.Handle("/api/users", securityHeadersMiddleware(handlers.AdminMiddleware(handlers.HandleUsers)))

	// Switch Config
	http.Handle("/api/switches", securityHeadersMiddleware(handlers.AuthMiddleware(handlers.HandleSwitchesWithRoleCheck)))
	http.Handle("/api/switches/sync", securityHeadersMiddleware(handlers.AdminMiddleware(handlers.HandleSwitchSync)))
	http.Handle("/api/switches/status", securityHeadersMiddleware(handlers.AuthMiddleware(handlers.HandleSwitchStatus)))

	// Static Files
	fs := http.FileServer(http.Dir("./web/dist"))
	http.Handle("/", securityHeadersMiddleware(gzipHandler(fs, "./web/dist")))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	certFile := os.Getenv("CERT_FILE")
	if certFile == "" {
		certFile = "server.crt"
	}
	keyFile := os.Getenv("KEY_FILE")
	if keyFile == "" {
		keyFile = "server.key"
	}

	if _, err := os.Stat(certFile); os.IsNotExist(err) {
		log.Printf("Generating self-signed certificate...")
		if err := generateSelfSignedCert(certFile, keyFile); err != nil {
			log.Fatalf("Failed to generate self-signed certificate: %v", err)
		}
	}

	// Create the main listener
	l, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatal(err)
	}

	// Create a multiplexer
	m := cmux.New(l)

	// Match TLS (HTTPS) and otherwise (HTTP)
	httpsL := m.Match(cmux.TLS())
	httpL := m.Match(cmux.Any())

	// HTTPS Server
	go func() {
		log.Printf("Starting HTTPS server on :%s", port)
		if err := http.ServeTLS(httpsL, nil, certFile, keyFile); err != nil {
			log.Fatalf("HTTPS server failed: %v", err)
		}
	}()

	// HTTP Redirect Server (on the same port)
	go func() {
		log.Printf("Starting HTTP redirect server on :%s", port)
		err := http.Serve(httpL, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host := r.Host

			target := fmt.Sprintf("https://%s%s", host, r.URL.Path)
			if len(r.URL.RawQuery) > 0 {
				target += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, target, http.StatusMovedPermanently)
		}))
		if err != nil {
			log.Fatalf("HTTP redirect server failed: %v", err)
		}
	}()

	log.Printf("Network Device Visualizer v%s started on :%s (HTTP & HTTPS)", Version, port)
	if err := m.Serve(); err != nil {
		log.Fatal(err)
	}
}

func generateSelfSignedCert(certFile, keyFile string) error {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Network Device Visualizer"},
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(365 * 24 * time.Hour),

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return err
	}

	certOut, err := os.Create(certFile)
	if err != nil {
		return err
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return err
	}

	keyOut, err := os.Create(keyFile)
	if err != nil {
		return err
	}
	defer keyOut.Close()
	if err := pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}); err != nil {
		return err
	}

	return nil
}

func gzipHandler(next http.Handler, root string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		gzPath := root + path + ".gz"
		if _, err := os.Stat(gzPath); err == nil {
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Content-Type", mimeType(path))
			http.ServeFile(w, r, gzPath)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:;")

		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")

		w.Header().Set("X-Content-Type-Options", "nosniff")

		w.Header().Set("X-Frame-Options", "DENY")

		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		next.ServeHTTP(w, r)
	})
}

func mimeType(path string) string {
	switch {
	case strings.HasSuffix(path, ".html"):
		return "text/html"
	case strings.HasSuffix(path, ".css"):
		return "text/css"
	case strings.HasSuffix(path, ".js"):
		return "application/javascript"
	case strings.HasSuffix(path, ".json"):
		return "application/json"
	case strings.HasSuffix(path, ".png"):
		return "image/png"
	case strings.HasSuffix(path, ".jpg"):
		return "image/jpeg"
	default:
		return "application/octet-stream"
	}
}
