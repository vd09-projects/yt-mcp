// yt-authorize performs the one-time 3-legged OAuth2 consent grant for ONE
// channel (spec §5.1: one refresh token per channel, obtained via a one-time
// consent grant) and prints the refresh token to store in config/env.
//
// Usage:
//
//	yt-authorize --config config.json
//	yt-authorize --client-id ... --client-secret ...
//
// Run it once per channel. Sign in with the Google account that manages the
// target channel and, if prompted, pick the correct brand/channel account —
// the tool prints which channel the token actually controls so tokens don't
// end up under the wrong alias.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"

	"yt-mcp/internal/config"
	"yt-mcp/internal/uploader"
)

func main() {
	log.SetFlags(0)

	cfgPath := flag.String("config", "", "optional config.json to read the OAuth client credentials from")
	clientID := flag.String("client-id", os.Getenv("YT_CLIENT_ID"), "OAuth client id (or set YT_CLIENT_ID)")
	clientSecret := flag.String("client-secret", os.Getenv("YT_CLIENT_SECRET"), "OAuth client secret (or set YT_CLIENT_SECRET)")
	extraScopes := flag.String("scopes", "", "comma-separated EXTRA OAuth scopes to request beyond the upload defaults, e.g. https://www.googleapis.com/auth/yt-analytics.readonly for a future analytics server. Prefer minting a SEPARATE token per capability (least privilege) over one token with every scope")
	flag.Parse()

	if *cfgPath != "" {
		// OAuth-only load: on first run the per-channel refresh tokens don't
		// exist yet, so full config validation would (correctly) fail.
		oc, err := config.LoadOAuthOnly(*cfgPath)
		if err != nil {
			log.Fatal(err)
		}
		*clientID = oc.ClientID
		*clientSecret = oc.ClientSecret
	}
	if *clientID == "" || *clientSecret == "" {
		log.Fatal("client credentials required: pass --config, or --client-id/--client-secret, or set YT_CLIENT_ID/YT_CLIENT_SECRET")
	}

	// Loopback redirect on a random port — allowed for "Desktop app" OAuth
	// clients without pre-registering the port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("listen on loopback: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	scopes := append([]string{}, uploader.Scopes...)
	if *extraScopes != "" {
		seen := map[string]bool{}
		for _, s := range scopes {
			seen[s] = true
		}
		for _, s := range strings.Split(*extraScopes, ",") {
			s = strings.TrimSpace(s)
			if s != "" && !seen[s] {
				scopes = append(scopes, s)
				seen[s] = true
			}
		}
		log.Printf("requesting %d scope(s): %s", len(scopes), strings.Join(scopes, " "))
	}

	oc := &oauth2.Config{
		ClientID:     *clientID,
		ClientSecret: *clientSecret,
		Endpoint:     google.Endpoint,
		RedirectURL:  fmt.Sprintf("http://127.0.0.1:%d/callback", port),
		Scopes:       scopes,
	}

	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		log.Fatalf("generate state: %v", err)
	}
	state := hex.EncodeToString(stateBytes)

	// AccessTypeOffline + prompt=consent guarantees a refresh token is
	// issued even if this client has been consented before.
	authURL := oc.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"))

	fmt.Println("Open this URL in a browser and complete the consent flow.")
	fmt.Println("IMPORTANT: choose the Google account / brand account that owns the TARGET channel.")
	fmt.Println()
	fmt.Println(authURL)
	fmt.Println()

	codeCh := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		if e := r.URL.Query().Get("error"); e != "" {
			http.Error(w, "consent error: "+e, http.StatusBadRequest)
			codeCh <- ""
			return
		}
		fmt.Fprintln(w, "Authorization received — you can close this tab and return to the terminal.")
		codeCh <- r.URL.Query().Get("code")
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)

	var code string
	select {
	case code = <-codeCh:
	case <-time.After(10 * time.Minute):
		log.Fatal("timed out waiting for the OAuth callback")
	}
	if code == "" {
		log.Fatal("consent was denied or failed")
	}

	ctx := context.Background()
	tok, err := oc.Exchange(ctx, code)
	if err != nil {
		log.Fatalf("authorization code exchange failed: %v", err)
	}
	if tok.RefreshToken == "" {
		log.Fatal("no refresh token was returned; retry the flow (prompt=consent is forced, so this is unexpected)")
	}

	// Show which channel this token actually controls, so it doesn't get
	// pasted under the wrong alias in the config.
	svc, err := youtube.NewService(ctx, option.WithTokenSource(oc.TokenSource(ctx, tok)))
	if err != nil {
		log.Fatalf("build youtube client: %v", err)
	}
	resp, err := svc.Channels.List([]string{"snippet"}).Mine(true).Do()
	if err != nil {
		log.Fatalf("channels.list failed (token was issued, but verification failed): %v", err)
	}
	if len(resp.Items) > 0 {
		fmt.Printf("\nAuthorized channel: %s (id: %s)\n", resp.Items[0].Snippet.Title, resp.Items[0].Id)
	} else {
		fmt.Println("\nToken issued, but no YouTube channel is attached to this account.")
	}

	fmt.Println("\nRefresh token — store it as the env var referenced from config.json for this channel:")
	fmt.Println()
	fmt.Println(tok.RefreshToken)
	fmt.Println()
	fmt.Println("Reminder (spec §5.1): while the OAuth consent screen is in \"Testing\" publishing status,")
	fmt.Println("this token expires in ~7 days and this flow must be re-run. Complete Google app")
	fmt.Println("verification (\"In production\") for long-lived tokens.")

	shutdownCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
