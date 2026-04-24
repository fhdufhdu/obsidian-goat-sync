package dashboard

import (
	"encoding/json"
	"net/http"

	"obsidian-sync/internal/config"
	"obsidian-sync/internal/db"
	"obsidian-sync/internal/storage"
)

type Dashboard struct {
	cfg      config.Config
	queries  *db.Queries
	storage  *storage.Storage
	sessions *SessionStore
}

func New(cfg config.Config, q *db.Queries, s *storage.Storage) *Dashboard {
	return &Dashboard{
		cfg:      cfg,
		queries:  q,
		storage:  s,
		sessions: NewSessionStore(),
	}
}

func (d *Dashboard) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/login", d.handleLogin)
	mux.HandleFunc("/logout", d.handleLogout)

	authed := d.sessions.AuthMiddleware
	mux.Handle("/", authed(http.HandlerFunc(d.handleIndex)))
	mux.Handle("/api/vaults", authed(http.HandlerFunc(d.handleVaults)))
	mux.Handle("/api/vaults/", authed(http.HandlerFunc(d.handleVaultSub)))
	mux.Handle("/api/tokens", authed(http.HandlerFunc(d.handleTokens)))
}

func (d *Dashboard) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/html")
		loginTemplate.Execute(w, nil)
		return
	}

	user := r.FormValue("username")
	pass := r.FormValue("password")
	if user != d.cfg.AdminUser || pass != d.cfg.AdminPass {
		w.Header().Set("Content-Type", "text/html")
		loginTemplate.Execute(w, map[string]string{"Error": "Invalid credentials"})
		return
	}

	token, _ := d.sessions.Create()
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (d *Dashboard) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("session"); err == nil {
		d.sessions.Delete(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "session", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (d *Dashboard) handleIndex(w http.ResponseWriter, r *http.Request) {
	vaults, _ := d.queries.ListVaults()

	type vaultInfo struct {
		Name       string
		InsertedAt string
		FileCount  int
		TotalSize  int64
	}
	var infos []vaultInfo
	for _, v := range vaults {
		count, size, _ := d.storage.VaultStats(v.Name)
		infos = append(infos, vaultInfo{
			Name:       v.Name,
			InsertedAt: v.InsertedAt,
			FileCount:  count,
			TotalSize:  size,
		})
	}

	w.Header().Set("Content-Type", "text/html")
	indexTemplate.Execute(w, infos)
}

func (d *Dashboard) handleVaults(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		vaults, _ := d.queries.ListVaults()
		json.NewEncoder(w).Encode(vaults)
	case http.MethodPost:
		var req struct{ Name string }
		json.NewDecoder(r.Body).Decode(&req)
		d.queries.CreateVault(req.Name)
		d.storage.CreateVaultDir(req.Name)
		w.WriteHeader(http.StatusCreated)
	case http.MethodDelete:
		name := r.URL.Query().Get("name")
		d.queries.DeleteVault(name)
		d.storage.DeleteVaultDir(name)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (d *Dashboard) handleVaultSub(w http.ResponseWriter, r *http.Request) {
	parts := splitPath(r.URL.Path)
	if len(parts) < 3 {
		http.NotFound(w, r)
		return
	}
	vaultName := parts[2]

	if len(parts) >= 4 && parts[3] == "github" {
		d.handleVaultGitHub(w, r, vaultName)
		return
	}
	if len(parts) >= 4 && parts[3] == "files" {
		d.handleVaultFiles(w, r, vaultName)
		return
	}
	http.NotFound(w, r)
}

func (d *Dashboard) handleVaultGitHub(w http.ResponseWriter, r *http.Request, vaultName string) {
	switch r.Method {
	case http.MethodGet:
		cfg, err := d.queries.GetGitHubConfig(vaultName)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		type maskedConfig struct {
			VaultName   string `json:"vault_name"`
			RemoteURL   string `json:"remote_url"`
			Branch      string `json:"branch"`
			Interval    string `json:"interval"`
			AccessToken string `json:"access_token"`
			AuthorName  string `json:"author_name"`
			AuthorEmail string `json:"author_email"`
			Enabled     bool   `json:"enabled"`
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(maskedConfig{
			VaultName:   cfg.VaultName,
			RemoteURL:   cfg.RemoteURL,
			Branch:      cfg.Branch,
			Interval:    cfg.Interval,
			AccessToken: cfg.MaskedAccessToken(),
			AuthorName:  cfg.AuthorName,
			AuthorEmail: cfg.AuthorEmail,
			Enabled:     cfg.Enabled,
		})
	case http.MethodPut:
		var req struct {
			RemoteURL   string `json:"remote_url"`
			Branch      string `json:"branch"`
			Interval    string `json:"interval"`
			AccessToken string `json:"access_token"`
			AuthorName  string `json:"author_name"`
			AuthorEmail string `json:"author_email"`
			Enabled     bool   `json:"enabled"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		existing, _ := d.queries.GetGitHubConfig(vaultName)
		accessToken := req.AccessToken
		if accessToken == "" {
			accessToken = existing.AccessToken
		}

		cfg := db.GitHubConfig{
			VaultName:   vaultName,
			RemoteURL:   req.RemoteURL,
			Branch:      req.Branch,
			Interval:    req.Interval,
			AccessToken: accessToken,
			AuthorName:  req.AuthorName,
			AuthorEmail: req.AuthorEmail,
			Enabled:     req.Enabled,
		}
		d.queries.SetGitHubConfig(cfg)
		w.WriteHeader(http.StatusOK)
	}
}

func (d *Dashboard) handleVaultFiles(w http.ResponseWriter, r *http.Request, vaultName string) {
	files, err := d.queries.ListActiveFiles(vaultName)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(files)
}

func (d *Dashboard) handleTokens(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		tokens, _ := d.queries.ListTokens()
		json.NewEncoder(w).Encode(tokens)
	case http.MethodPost:
		action := r.URL.Query().Get("action")
		if action == "regenerate" {
			old := r.URL.Query().Get("token")
			newToken, _ := d.queries.RegenerateToken(old)
			json.NewEncoder(w).Encode(map[string]string{"token": newToken})
		} else {
			token, _ := d.queries.GenerateToken()
			json.NewEncoder(w).Encode(map[string]string{"token": token})
		}
	case http.MethodDelete:
		token := r.URL.Query().Get("token")
		d.queries.DeactivateToken(token)
		w.WriteHeader(http.StatusNoContent)
	}
}

func splitPath(path string) []string {
	var parts []string
	for _, p := range split(path, '/') {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func split(s string, sep byte) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	result = append(result, s[start:])
	return result
}
