package dashboard

import "html/template"

var loginTemplate = template.Must(template.New("login").Parse(`<!DOCTYPE html>
<html><head><title>Obsidian Goat Sync - Login</title>
<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: system-ui, -apple-system, sans-serif; background: #0f0f0f; color: #e0e0e0; }
.login-wrap { max-width: 380px; margin: 120px auto; padding: 0 20px; }
h1 { color: #a78bfa; margin-bottom: 24px; }
form { display: flex; flex-direction: column; gap: 12px; }
input { padding: 10px 12px; border: 1px solid #333; border-radius: 6px; background: #1a1a1a; color: #e0e0e0; font-size: 14px; }
input:focus { outline: none; border-color: #7c3aed; }
button { padding: 10px; background: #7c3aed; color: white; border: none; border-radius: 6px; cursor: pointer; font-size: 14px; font-weight: 500; }
button:hover { background: #6d28d9; }
.error { color: #f87171; font-size: 14px; margin-bottom: 8px; }
</style></head>
<body>
<div class="login-wrap">
<h1>Obsidian Goat Sync</h1>
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}
<form method="POST" action="/login">
<input name="username" placeholder="Username" required>
<input name="password" type="password" placeholder="Password" required>
<button type="submit">Login</button>
</form>
</div>
</body></html>`))

var indexTemplate = template.Must(template.New("index").Parse(`<!DOCTYPE html>
<html><head><title>Obsidian Goat Sync</title>
<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: system-ui, -apple-system, sans-serif; background: #0f0f0f; color: #e0e0e0; }
nav { background: #1a1a1a; border-bottom: 1px solid #262626; padding: 12px 24px; display: flex; align-items: center; justify-content: space-between; }
nav .brand { color: #a78bfa; font-weight: 600; font-size: 16px; }
nav a { color: #888; text-decoration: none; font-size: 14px; }
nav a:hover { color: #e0e0e0; }
.container { max-width: 960px; margin: 0 auto; padding: 24px; }
.tabs { display: flex; gap: 4px; margin-bottom: 24px; }
.tab { padding: 8px 16px; border-radius: 6px; cursor: pointer; font-size: 14px; background: transparent; color: #888; border: none; }
.tab.active { background: #7c3aed; color: white; }
.tab:hover:not(.active) { color: #e0e0e0; }
.section { display: none; }
.section.active { display: block; }
h2 { font-size: 18px; margin-bottom: 16px; color: #e0e0e0; }
.card { background: #1a1a1a; border: 1px solid #262626; border-radius: 8px; padding: 16px; margin-bottom: 12px; }
table { width: 100%; border-collapse: collapse; }
th { text-align: left; padding: 8px 12px; color: #888; font-size: 12px; text-transform: uppercase; letter-spacing: 0.5px; border-bottom: 1px solid #262626; }
td { padding: 10px 12px; border-bottom: 1px solid #1f1f1f; font-size: 14px; }
.mono { font-family: 'SF Mono', Monaco, monospace; font-size: 12px; color: #a78bfa; word-break: break-all; }
.badge { display: inline-block; padding: 2px 8px; border-radius: 10px; font-size: 11px; font-weight: 500; }
.badge.active { background: #065f46; color: #6ee7b7; }
.badge.inactive { background: #451a1a; color: #f87171; }
.row { display: flex; gap: 8px; align-items: center; margin-bottom: 16px; }
input[type="text"] { padding: 8px 12px; border: 1px solid #333; border-radius: 6px; background: #0f0f0f; color: #e0e0e0; font-size: 14px; flex: 1; }
input[type="text"]:focus { outline: none; border-color: #7c3aed; }
button { padding: 8px 14px; border: none; border-radius: 6px; cursor: pointer; font-size: 13px; font-weight: 500; }
.btn-primary { background: #7c3aed; color: white; }
.btn-primary:hover { background: #6d28d9; }
.btn-danger { background: #dc2626; color: white; }
.btn-danger:hover { background: #b91c1c; }
.btn-sm { padding: 4px 10px; font-size: 12px; }
.empty { color: #555; font-size: 14px; padding: 20px 0; text-align: center; }
.toast { position: fixed; bottom: 20px; right: 20px; background: #1a1a1a; border: 1px solid #333; border-radius: 8px; padding: 12px 20px; font-size: 14px; display: none; z-index: 100; }
.toast.success { border-color: #065f46; color: #6ee7b7; }
.toast.error { border-color: #dc2626; color: #f87171; }
</style></head>
<body>
<nav>
  <span class="brand">Obsidian Goat Sync</span>
  <a href="/logout">Logout</a>
</nav>
<div class="container">
  <div class="tabs">
    <button class="tab active" onclick="showTab('vaults')">Vaults</button>
    <button class="tab" onclick="showTab('tokens')">Tokens</button>
  </div>

  <div id="vaults" class="section active">
    <h2>Vaults</h2>
    <div class="row">
      <input type="text" id="vault-name" placeholder="New vault name">
      <button class="btn-primary" onclick="createVault()">Create</button>
    </div>
    <div class="card">
      <table>
        <thead><tr><th>Name</th><th>Files</th><th>Size</th><th>Created</th><th></th></tr></thead>
        <tbody id="vault-list">
        {{range .}}
        <tr data-vault="{{.Name}}">
          <td><a href="#" onclick="showFiles('{{.Name}}');return false;" style="color:#a78bfa;text-decoration:none;">{{.Name}}</a></td>
          <td>{{.FileCount}}</td>
          <td>{{.TotalSize}} B</td>
          <td>{{.InsertedAt}}</td>
          <td>
            <button class="btn-primary btn-sm" onclick="showGitHub('{{.Name}}')" style="margin-right:4px;">GitHub</button>
            <button class="btn-danger btn-sm" onclick="deleteVault('{{.Name}}')">Delete</button>
          </td>
        </tr>
        {{else}}
        <tr id="no-vaults"><td colspan="5" class="empty">No vaults yet</td></tr>
        {{end}}
        </tbody>
      </table>
    </div>
  </div>

  <div id="files" class="section">
    <h2>Files: <span id="files-vault-name"></span></h2>
    <div style="margin-bottom:12px;"><button class="btn-primary btn-sm" onclick="showTab('vaults')">&larr; Back to Vaults</button></div>
    <div class="card">
      <table>
        <thead><tr><th>Path</th><th>Version / Updated</th></tr></thead>
        <tbody id="file-list"></tbody>
      </table>
    </div>
  </div>

  <div id="github" class="section">
    <h2>GitHub Backup: <span id="github-vault-name"></span></h2>
    <div style="margin-bottom:12px;"><button class="btn-primary btn-sm" onclick="showTab('vaults')">&larr; Back to Vaults</button></div>
    <div class="card">
      <div style="display:flex;flex-direction:column;gap:12px;max-width:500px;">
        <div><label style="display:block;margin-bottom:4px;font-size:13px;">Remote URL</label><input type="text" id="gh-remote-url" placeholder="https://github.com/user/vault.git" style="width:100%;"></div>
        <div><label style="display:block;margin-bottom:4px;font-size:13px;">Branch</label><input type="text" id="gh-branch" placeholder="main" style="width:100%;"></div>
        <div><label style="display:block;margin-bottom:4px;font-size:13px;">Interval (Go duration)</label><input type="text" id="gh-interval" placeholder="1h" style="width:100%;"></div>
        <div><label style="display:block;margin-bottom:4px;font-size:13px;">Access Token</label><input type="password" id="gh-access-token" placeholder="ghp_... (leave empty to keep current)" style="width:100%;"></div>
        <div><label style="display:block;margin-bottom:4px;font-size:13px;">Author Name</label><input type="text" id="gh-author-name" placeholder="Your Name" style="width:100%;"></div>
        <div><label style="display:block;margin-bottom:4px;font-size:13px;">Author Email</label><input type="text" id="gh-author-email" placeholder="you@example.com" style="width:100%;"></div>
        <div style="display:flex;align-items:center;gap:8px;"><input type="checkbox" id="gh-enabled"><label for="gh-enabled" style="font-size:13px;">Enabled</label></div>
        <div><button class="btn-primary" onclick="saveGitHub()">Save</button></div>
      </div>
    </div>
  </div>

  <div id="tokens" class="section">
    <h2>API Tokens</h2>
    <div class="row">
      <button class="btn-primary" onclick="generateToken()">Generate New Token</button>
    </div>
    <div class="card">
      <table>
        <thead><tr><th>Token</th><th>Status</th><th>Created</th><th></th></tr></thead>
        <tbody id="token-list"></tbody>
      </table>
    </div>
  </div>
</div>
<div id="toast" class="toast"></div>

<script>
function showTab(name) {
  document.querySelectorAll('.section').forEach(s => s.classList.remove('active'));
  document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
  document.getElementById(name).classList.add('active');
  const btn = document.querySelector('.tab[onclick*="'+name+'"]');
  if (btn) btn.classList.add('active');
  if (name === 'tokens') loadTokens();
}

async function showFiles(vaultName) {
  document.querySelectorAll('.section').forEach(s => s.classList.remove('active'));
  document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
  document.getElementById('files').classList.add('active');
  document.getElementById('files-vault-name').textContent = vaultName;
  const res = await fetch('/api/vaults/' + encodeURIComponent(vaultName) + '/files');
  const files = await res.json();
  const tbody = document.getElementById('file-list');
  if (!files || files.length === 0) {
    tbody.innerHTML = '<tr><td colspan="2" class="empty">No files</td></tr>';
    return;
  }
  tbody.innerHTML = files.map(f =>
    '<tr><td class="mono">' + f.Path + '</td><td>v' + f.Version + ' ' + f.UpdatedAt + '</td></tr>'
  ).join('');
}

function toast(msg, type) {
  const t = document.getElementById('toast');
  t.textContent = msg;
  t.className = 'toast ' + type;
  t.style.display = 'block';
  setTimeout(() => t.style.display = 'none', 3000);
}

async function createVault() {
  const input = document.getElementById('vault-name');
  const name = input.value.trim();
  if (!name) return;
  const res = await fetch('/api/vaults', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify({Name: name}) });
  if (res.ok) { toast('Vault created', 'success'); input.value = ''; location.reload(); }
  else toast('Failed to create vault', 'error');
}

async function deleteVault(name) {
  if (!confirm('Delete vault "' + name + '"? This removes all files.')) return;
  const res = await fetch('/api/vaults?name=' + encodeURIComponent(name), { method: 'DELETE' });
  if (res.ok) { toast('Vault deleted', 'success'); document.querySelector('tr[data-vault="'+name+'"]').remove(); }
  else toast('Failed to delete vault', 'error');
}

async function loadTokens() {
  const res = await fetch('/api/tokens');
  const tokens = await res.json();
  const tbody = document.getElementById('token-list');
  if (!tokens || tokens.length === 0) {
    tbody.innerHTML = '<tr><td colspan="4" class="empty">No tokens yet</td></tr>';
    return;
  }
  tbody.innerHTML = tokens.map(t =>
    '<tr>' +
    '<td class="mono">' + t.Token + '</td>' +
    '<td><span class="badge ' + (t.IsActive ? 'active' : 'inactive') + '">' + (t.IsActive ? 'Active' : 'Revoked') + '</span></td>' +
    '<td>' + t.InsertedAt + '</td>' +
    '<td>' + (t.IsActive ? '<button class="btn-danger btn-sm" onclick="revokeToken(\'' + t.Token + '\')">Revoke</button>' : '') + '</td>' +
    '</tr>'
  ).join('');
}

async function generateToken() {
  const res = await fetch('/api/tokens', { method: 'POST' });
  const data = await res.json();
  if (data.token) {
    toast('Token: ' + data.token, 'success');
    loadTokens();
  }
}

async function revokeToken(token) {
  if (!confirm('Revoke this token?')) return;
  const res = await fetch('/api/tokens?token=' + encodeURIComponent(token), { method: 'DELETE' });
  if (res.ok) { toast('Token revoked', 'success'); loadTokens(); }
}

let currentGitHubVault = '';

async function showGitHub(vaultName) {
  currentGitHubVault = vaultName;
  document.querySelectorAll('.section').forEach(s => s.classList.remove('active'));
  document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
  document.getElementById('github').classList.add('active');
  document.getElementById('github-vault-name').textContent = vaultName;

  const res = await fetch('/api/vaults/' + encodeURIComponent(vaultName) + '/github');
  if (res.ok) {
    const cfg = await res.json();
    document.getElementById('gh-remote-url').value = cfg.remote_url || '';
    document.getElementById('gh-branch').value = cfg.branch || 'main';
    document.getElementById('gh-interval').value = cfg.interval || '1h';
    document.getElementById('gh-access-token').value = '';
    document.getElementById('gh-access-token').placeholder = cfg.access_token ? 'Leave empty to keep: ' + cfg.access_token : 'ghp_...';
    document.getElementById('gh-author-name').value = cfg.author_name || '';
    document.getElementById('gh-author-email').value = cfg.author_email || '';
    document.getElementById('gh-enabled').checked = cfg.enabled;
  } else {
    document.getElementById('gh-remote-url').value = '';
    document.getElementById('gh-branch').value = 'main';
    document.getElementById('gh-interval').value = '1h';
    document.getElementById('gh-access-token').value = '';
    document.getElementById('gh-author-name').value = '';
    document.getElementById('gh-author-email').value = '';
    document.getElementById('gh-enabled').checked = true;
  }
}

async function saveGitHub() {
  const body = {
    remote_url: document.getElementById('gh-remote-url').value,
    branch: document.getElementById('gh-branch').value,
    interval: document.getElementById('gh-interval').value,
    access_token: document.getElementById('gh-access-token').value,
    author_name: document.getElementById('gh-author-name').value,
    author_email: document.getElementById('gh-author-email').value,
    enabled: document.getElementById('gh-enabled').checked,
  };
  const res = await fetch('/api/vaults/' + encodeURIComponent(currentGitHubVault) + '/github', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (res.ok) toast('GitHub config saved', 'success');
  else toast('Failed to save GitHub config', 'error');
}
</script>
</body></html>`))
