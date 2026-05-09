package server

// uiHTML is the embedded web UI for Orchestra agent interactions.
// It is a self-contained single-page application with no external dependencies.
const uiHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Orchestra — Agent Hub</title>
<style>
:root {
  --bg: #0f1117;
  --surface: #1a1d27;
  --surface2: #232733;
  --border: #2e3345;
  --text: #e1e4ed;
  --text2: #8b90a5;
  --accent: #6c5ce7;
  --accent2: #a29bfe;
  --green: #00cec9;
  --red: #ff6b6b;
  --orange: #feca57;
  --blue: #74b9ff;
  --radius: 10px;
  --font: 'Segoe UI', system-ui, -apple-system, sans-serif;
  --mono: 'SF Mono', 'Cascadia Code', 'Consolas', monospace;
}
*, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
html, body { height: 100%; }
body {
  font-family: var(--font);
  background: var(--bg);
  color: var(--text);
  line-height: 1.6;
}
a { color: var(--accent2); text-decoration: none; }
a:hover { text-decoration: underline; }

/* Layout */
.app { display: flex; height: 100vh; }
.sidebar {
  width: 300px;
  min-width: 260px;
  background: var(--surface);
  border-right: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.main { flex: 1; display: flex; flex-direction: column; overflow: hidden; }

/* Sidebar */
.sidebar-header {
  padding: 20px;
  border-bottom: 1px solid var(--border);
}
.sidebar-header h1 {
  font-size: 18px;
  font-weight: 700;
  display: flex;
  align-items: center;
  gap: 8px;
}
.sidebar-header h1 .logo {
  width: 24px; height: 24px;
  background: var(--accent);
  border-radius: 6px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 14px;
}
.sidebar-section {
  padding: 16px 20px 8px;
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 1px;
  color: var(--text2);
  font-weight: 600;
}
.agent-list {
  flex: 1;
  overflow-y: auto;
  padding: 0 12px 12px;
}
.agent-card {
  padding: 12px 14px;
  border-radius: var(--radius);
  cursor: pointer;
  transition: background 0.15s;
  margin-bottom: 4px;
  border: 1px solid transparent;
}
.agent-card:hover { background: var(--surface2); }
.agent-card.active {
  background: var(--surface2);
  border-color: var(--accent);
}
.agent-card .agent-name {
  font-weight: 600;
  font-size: 14px;
  margin-bottom: 2px;
}
.agent-card .agent-meta {
  font-size: 12px;
  color: var(--text2);
  display: flex;
  gap: 8px;
  align-items: center;
}
.agent-card .agent-meta .badge {
  padding: 1px 6px;
  border-radius: 4px;
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
}
.badge-provider { background: #6c5ce733; color: var(--accent2); }
.badge-model { background: #00cec933; color: var(--green); }

/* New agent button */
.btn-new-agent {
  margin: 12px 16px 16px;
  padding: 10px;
  border: 1px dashed var(--border);
  border-radius: var(--radius);
  background: transparent;
  color: var(--text2);
  font-size: 13px;
  cursor: pointer;
  transition: all 0.15s;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
}
.btn-new-agent:hover {
  border-color: var(--accent);
  color: var(--accent2);
  background: #6c5ce708;
}

/* Main area */
.main-header {
  padding: 16px 24px;
  border-bottom: 1px solid var(--border);
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: var(--surface);
}
.main-header h2 {
  font-size: 16px;
  font-weight: 600;
  display: flex;
  align-items: center;
  gap: 10px;
}
.main-header .header-actions { display: flex; gap: 8px; }

/* Buttons */
.btn {
  padding: 8px 16px;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: var(--surface2);
  color: var(--text);
  font-size: 13px;
  cursor: pointer;
  transition: all 0.15s;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-family: var(--font);
}
.btn:hover { border-color: var(--text2); }
.btn-primary {
  background: var(--accent);
  border-color: var(--accent);
  color: #fff;
}
.btn-primary:hover { background: #5a4bd6; }
.btn-danger { color: var(--red); }
.btn-danger:hover { background: #ff6b6b15; border-color: var(--red); }
.btn-sm { padding: 5px 10px; font-size: 12px; }

/* Chat area */
.chat-area {
  flex: 1;
  overflow-y: auto;
  padding: 24px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.empty-state {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  color: var(--text2);
  gap: 12px;
}
.empty-state .icon { font-size: 48px; opacity: 0.4; }
.empty-state h3 { font-size: 18px; color: var(--text); }
.empty-state p { font-size: 14px; max-width: 400px; text-align: center; }

/* Messages */
.message {
  max-width: 760px;
  width: 100%;
  align-self: flex-start;
}
.message.user { align-self: flex-end; }
.message-inner {
  padding: 14px 18px;
  border-radius: 12px;
  position: relative;
}
.message.user .message-inner {
  background: var(--accent);
  color: #fff;
  border-bottom-right-radius: 4px;
}
.message.assistant .message-inner {
  background: var(--surface2);
  border-bottom-left-radius: 4px;
}
.message.system .message-inner {
  background: #feca5715;
  border: 1px solid #feca5733;
  font-size: 13px;
}
.message.error .message-inner {
  background: #ff6b6b15;
  border: 1px solid #ff6b6b33;
  color: var(--red);
}
.message-role {
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: var(--text2);
  margin-bottom: 4px;
  font-weight: 600;
}
.message.user .message-role { color: #ffffffaa; }
.message-content {
  font-size: 14px;
  white-space: pre-wrap;
  word-break: break-word;
}
.message-content code {
  font-family: var(--mono);
  background: #00000040;
  padding: 2px 6px;
  border-radius: 4px;
  font-size: 13px;
}
.message-meta {
  margin-top: 6px;
  font-size: 11px;
  color: var(--text2);
  display: flex;
  gap: 12px;
}
.message.user .message-meta { color: #ffffff77; }
.tool-call-badge {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 8px;
  border-radius: 4px;
  background: #00cec920;
  color: var(--green);
  font-size: 11px;
  font-weight: 600;
  margin-top: 6px;
}
.typing-indicator {
  display: flex;
  gap: 4px;
  padding: 8px 0;
}
.typing-indicator span {
  width: 8px; height: 8px;
  background: var(--text2);
  border-radius: 50%;
  animation: blink 1.4s infinite both;
}
.typing-indicator span:nth-child(2) { animation-delay: 0.2s; }
.typing-indicator span:nth-child(3) { animation-delay: 0.4s; }
@keyframes blink {
  0%, 80%, 100% { opacity: 0.2; transform: scale(0.8); }
  40% { opacity: 1; transform: scale(1); }
}

/* Input area */
.input-area {
  padding: 16px 24px 20px;
  border-top: 1px solid var(--border);
  background: var(--surface);
}
.input-row {
  display: flex;
  gap: 8px;
  align-items: flex-end;
}
.input-row textarea {
  flex: 1;
  padding: 12px 16px;
  border-radius: 12px;
  border: 1px solid var(--border);
  background: var(--surface2);
  color: var(--text);
  font-family: var(--font);
  font-size: 14px;
  resize: none;
  outline: none;
  min-height: 44px;
  max-height: 200px;
  transition: border-color 0.15s;
}
.input-row textarea:focus { border-color: var(--accent); }
.input-row textarea::placeholder { color: var(--text2); }

/* Modal */
.modal-overlay {
  position: fixed;
  inset: 0;
  background: #000000aa;
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 100;
  opacity: 0;
  pointer-events: none;
  transition: opacity 0.2s;
}
.modal-overlay.open { opacity: 1; pointer-events: all; }
.modal {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 16px;
  padding: 28px;
  width: 480px;
  max-width: 90vw;
  max-height: 85vh;
  overflow-y: auto;
}
.modal h2 { font-size: 18px; margin-bottom: 20px; }
.form-group { margin-bottom: 16px; }
.form-group label {
  display: block;
  font-size: 13px;
  font-weight: 600;
  margin-bottom: 6px;
  color: var(--text2);
}
.form-group input,
.form-group textarea,
.form-group select {
  width: 100%;
  padding: 10px 14px;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: var(--surface2);
  color: var(--text);
  font-family: var(--font);
  font-size: 14px;
  outline: none;
}
.form-group input:focus,
.form-group textarea:focus,
.form-group select:focus { border-color: var(--accent); }
.form-group textarea { min-height: 80px; resize: vertical; }
.form-group select { cursor: pointer; }
.form-group .hint { font-size: 12px; color: var(--text2); margin-top: 4px; }
.form-actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 20px; }

/* Scrollbar */
::-webkit-scrollbar { width: 6px; }
::-webkit-scrollbar-track { background: transparent; }
::-webkit-scrollbar-thumb { background: var(--border); border-radius: 3px; }
::-webkit-scrollbar-thumb:hover { background: var(--text2); }

/* Responsive */
@media (max-width: 768px) {
  .sidebar { width: 240px; min-width: 200px; }
}
@media (max-width: 600px) {
  .sidebar { display: none; }
}
</style>
</head>
<body>
<div class="app">
  <!-- Sidebar -->
  <div class="sidebar">
    <div class="sidebar-header">
      <h1><span class="logo">O</span> Orchestra</h1>
    </div>
    <div class="sidebar-section">Agents</div>
    <div class="agent-list" id="agentList">
      <!-- populated by JS -->
    </div>
    <button class="btn-new-agent" onclick="openNewAgentModal()">
      + New Agent
    </button>
  </div>

  <!-- Main Content -->
  <div class="main">
    <div class="main-header" id="mainHeader">
      <h2 id="mainTitle">Select or create an agent</h2>
      <div class="header-actions" id="headerActions"></div>
    </div>
    <div class="chat-area" id="chatArea">
      <div class="empty-state" id="emptyState">
        <div class="icon">&#x1F3B8;</div>
        <h3>Welcome to Orchestra</h3>
        <p>Create a new agent or select one from the sidebar to start interacting with AI models.</p>
      </div>
    </div>
    <div class="input-area" id="inputArea" style="display:none;">
      <div class="input-row">
        <textarea id="userInput" rows="1" placeholder="Send a message..."
                  onkeydown="handleInputKey(event)"></textarea>
        <button class="btn btn-primary" id="sendBtn" onclick="sendMessage()">Send</button>
      </div>
    </div>
  </div>
</div>

<!-- New Agent Modal -->
<div class="modal-overlay" id="newAgentModal">
  <div class="modal">
    <h2>Create New Agent</h2>
    <div class="form-group">
      <label for="agentName">Name *</label>
      <input type="text" id="agentName" placeholder="e.g. Code Helper, Research Assistant">
    </div>
    <div class="form-group">
      <label for="agentModel">Model</label>
      <input type="text" id="agentModel" placeholder="e.g. openai::gpt-4o, anthropic::claude-sonnet-4-20250514">
      <div class="hint">Format: provider::model or leave blank for default</div>
    </div>
    <div class="form-group">
      <label for="agentSystem">System Prompt</label>
      <textarea id="agentSystem" placeholder="You are a helpful assistant that..."></textarea>
    </div>
    <div class="form-group">
      <label for="agentMaxTurns">Max Turns</label>
      <input type="number" id="agentMaxTurns" value="10" min="1" max="100">
      <div class="hint">Maximum number of provider calls per run</div>
    </div>
    <div class="form-actions">
      <button class="btn" onclick="closeNewAgentModal()">Cancel</button>
      <button class="btn btn-primary" onclick="createAgent()">Create Agent</button>
    </div>
  </div>
</div>

<script>
// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------
const state = {
  agents: [],
  activeAgentId: null,
  conversations: {},  // agentId -> [{role, content, meta}]
  loading: false,
  providers: [],
  models: [],
};

const API = '/v1';

// ---------------------------------------------------------------------------
// API helpers
// ---------------------------------------------------------------------------
async function apiFetch(path, opts = {}) {
  const headers = { 'Content-Type': 'application/json', ...(opts.headers || {}) };
  const key = getAPIKey();
  if (key) headers['X-API-Key'] = key;
  const res = await fetch(API + path, { ...opts, headers });
  const data = await res.json();
  if (!res.ok) {
    const msg = data.error ? (data.error.message || JSON.stringify(data.error)) : res.statusText;
    throw new Error(msg);
  }
  return data;
}

function getAPIKey() {
  const params = new URLSearchParams(window.location.search);
  return params.get('key') || localStorage.getItem('orchestra_api_key') || '';
}

// ---------------------------------------------------------------------------
// Agent CRUD
// ---------------------------------------------------------------------------
async function loadAgents() {
  try {
    const data = await apiFetch('/agents');
    state.agents = data.agents || [];
    renderAgentList();
  } catch (e) {
    console.error('Failed to load agents:', e);
  }
}

async function loadProviders() {
  try {
    const data = await apiFetch('/providers');
    state.providers = data.providers || [];
  } catch (e) {
    console.error('Failed to load providers:', e);
  }
}

async function loadModels() {
  try {
    const data = await apiFetch('/models');
    state.models = data.models || [];
  } catch (e) {
    console.error('Failed to load models:', e);
  }
}

function renderAgentList() {
  const list = document.getElementById('agentList');
  if (state.agents.length === 0) {
    list.innerHTML = '<div style="padding:12px 8px;color:var(--text2);font-size:13px;">No agents yet. Create one to get started.</div>';
    return;
  }
  list.innerHTML = state.agents.map(a => {
    const active = a.id === state.activeAgentId ? ' active' : '';
    const provider = escHtml(a.provider || 'unknown');
    const model = escHtml(a.model || 'default');
    return '<div class="agent-card' + active + '" onclick="selectAgent(\'' + escAttr(a.id) + '\')">' +
      '<div class="agent-name">' + escHtml(a.name) + '</div>' +
      '<div class="agent-meta">' +
        '<span class="badge badge-provider">' + provider + '</span>' +
        '<span class="badge badge-model">' + model + '</span>' +
      '</div>' +
    '</div>';
  }).join('');
}

async function selectAgent(id) {
  state.activeAgentId = id;
  renderAgentList();
  renderChat();
  renderHeader();

  const inputArea = document.getElementById('inputArea');
  inputArea.style.display = 'flex';
  document.getElementById('userInput').focus();
}

async function createAgent() {
  const name = document.getElementById('agentName').value.trim();
  const model = document.getElementById('agentModel').value.trim();
  const system = document.getElementById('agentSystem').value.trim();
  const maxTurns = parseInt(document.getElementById('agentMaxTurns').value) || 10;

  if (!name) {
    alert('Agent name is required.');
    return;
  }

  try {
    const body = { name };
    if (model) body.model = model;
    if (system) body.system_prompt = system;
    if (maxTurns > 0) body.max_turns = maxTurns;

    const data = await apiFetch('/agents', {
      method: 'POST',
      body: JSON.stringify(body),
    });

    closeNewAgentModal();
    document.getElementById('agentName').value = '';
    document.getElementById('agentModel').value = '';
    document.getElementById('agentSystem').value = '';
    document.getElementById('agentMaxTurns').value = '10';

    await loadAgents();
    selectAgent(data.agent_id);
  } catch (e) {
    alert('Failed to create agent: ' + e.message);
  }
}

async function deleteAgent(id) {
  if (!confirm('Delete this agent? This cannot be undone.')) return;
  try {
    await apiFetch('/agents/' + encodeURIComponent(id), { method: 'DELETE' });
    if (state.activeAgentId === id) {
      state.activeAgentId = null;
      state.conversations[id] = [];
      renderChat();
    }
    await loadAgents();
  } catch (e) {
    alert('Failed to delete agent: ' + e.message);
  }
}

// ---------------------------------------------------------------------------
// Chat
// ---------------------------------------------------------------------------
function renderChat() {
  const area = document.getElementById('chatArea');
  const empty = document.getElementById('emptyState');
  const id = state.activeAgentId;

  if (!id) {
    area.innerHTML = '';
    area.appendChild(empty);
    empty.style.display = 'flex';
    return;
  }

  const msgs = state.conversations[id] || [];
  if (msgs.length === 0) {
    area.innerHTML = '';
    area.appendChild(empty);
    empty.style.display = 'flex';
    return;
  }

  empty.style.display = 'none';
  area.innerHTML = msgs.map((m, i) => renderMessage(m, i)).join('');
  area.scrollTop = area.scrollHeight;
}

function renderMessage(m, idx) {
  const cls = m.role === 'user' ? ' user' : m.role === 'error' ? ' error' : m.role === 'system' ? ' system' : ' assistant';
  const roleLabel = m.role === 'user' ? 'You' : m.role === 'error' ? 'Error' : m.role === 'system' ? 'System' : 'Agent';
  let meta = '';
  if (m.meta) {
    const parts = [];
    if (m.meta.duration) parts.push(formatDuration(m.meta.duration));
    if (m.meta.turns) parts.push(m.meta.turns + ' turn' + (m.meta.turns > 1 ? 's' : ''));
    if (m.meta.tokens) parts.push(m.meta.tokens + ' tokens');
    if (parts.length) meta = '<div class="message-meta">' + parts.join(' &middot; ') + '</div>';
  }
  let toolCalls = '';
  if (m.toolCalls && m.toolCalls.length > 0) {
    toolCalls = m.toolCalls.map(tc =>
      '<div class="tool-call-badge">&#x1F527; ' + escHtml(tc.function.name) + '</div>'
    ).join('');
  }
  return '<div class="message' + cls + '">' +
    '<div class="message-role">' + roleLabel + '</div>' +
    '<div class="message-inner">' +
      '<div class="message-content">' + renderContent(m.content) + '</div>' +
      toolCalls +
      meta +
    '</div>' +
  '</div>';
}

function renderContent(content) {
  if (!content) return '';
  // Basic markdown-ish rendering
  let text = escHtml(content);
  // Code blocks
  text = text.replace(/\x60\x60\x60([\s\S]*?)\x60\x60\x60/g, '<code>$1</code>');
  // Inline code
  text = text.replace(/\x60([^\x60]+)\x60/g, '<code>$1</code>');
  // Bold
  text = text.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>');
  // Newlines
  text = text.replace(/\n/g, '<br>');
  return text;
}

function addMessage(agentId, msg) {
  if (!state.conversations[agentId]) state.conversations[agentId] = [];
  state.conversations[agentId].push(msg);
  if (agentId === state.activeAgentId) {
    renderChat();
  }
}

function showTyping() {
  const area = document.getElementById('chatArea');
  const el = document.createElement('div');
  el.id = 'typing';
  el.className = 'message assistant';
  el.innerHTML = '<div class="message-role">Agent</div><div class="message-inner"><div class="typing-indicator"><span></span><span></span><span></span></div></div>';
  area.appendChild(el);
  area.scrollTop = area.scrollHeight;
}

function hideTyping() {
  const el = document.getElementById('typing');
  if (el) el.remove();
}

async function sendMessage() {
  const id = state.activeAgentId;
  if (!id) return;

  const input = document.getElementById('userInput');
  const text = input.value.trim();
  if (!text || state.loading) return;

  input.value = '';
  autoResize(input);
  state.loading = true;
  updateSendBtn();

  // Add user message
  addMessage(id, { role: 'user', content: text });

  showTyping();

  try {
    const data = await apiFetch('/agents/' + encodeURIComponent(id) + '/run', {
      method: 'POST',
      body: JSON.stringify({ input: text }),
    });

    hideTyping();

    const agent = state.agents.find(a => a.id === id);
    const result = data;
    const outputText = extractText(result);
    const meta = {};

    if (result.duration) meta.duration = result.duration;
    if (result.turns) meta.turns = result.turns;
    if (result.usage) {
      meta.tokens = (result.usage.total_tokens || 0);
    }

    const msg = { role: 'assistant', content: outputText, meta };
    if (result.tool_calls && result.tool_calls.length > 0) {
      msg.toolCalls = result.tool_calls;
    }
    addMessage(id, msg);
  } catch (e) {
    hideTyping();
    addMessage(id, { role: 'error', content: e.message });
  } finally {
    state.loading = false;
    updateSendBtn();
  }
}

function extractText(result) {
  if (typeof result.output === 'string') return result.output;
  if (result.output && result.output.content) {
    if (Array.isArray(result.output.content)) {
      return result.output.content.filter(c => c.type === 'text').map(c => c.text).join('');
    }
    if (typeof result.output.content === 'string') return result.output.content;
  }
  if (result.output && result.output.text) return result.output.text;
  if (result.message) {
    if (typeof result.message === 'string') return result.message;
    if (result.message.content) {
      if (Array.isArray(result.message.content)) {
        return result.message.content.filter(c => c.type === 'text').map(c => c.text).join('');
      }
      return String(result.message.content);
    }
  }
  return JSON.stringify(result, null, 2);
}

function handleInputKey(e) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault();
    sendMessage();
  }
}

function updateSendBtn() {
  const btn = document.getElementById('sendBtn');
  btn.disabled = state.loading;
  btn.textContent = state.loading ? 'Thinking...' : 'Send';
}

// ---------------------------------------------------------------------------
// Header
// ---------------------------------------------------------------------------
function renderHeader() {
  const title = document.getElementById('mainTitle');
  const actions = document.getElementById('headerActions');
  const id = state.activeAgentId;

  if (!id) {
    title.textContent = 'Select or create an agent';
    actions.innerHTML = '';
    return;
  }

  const agent = state.agents.find(a => a.id === id);
  if (!agent) {
    title.textContent = 'Agent not found';
    actions.innerHTML = '';
    return;
  }

  title.innerHTML = '<span style="color:var(--accent2)">' + escHtml(agent.name) + '</span>' +
    '<span style="color:var(--text2);font-weight:400;font-size:13px;">' +
    escHtml(agent.provider || '') + ' / ' + escHtml(agent.model || 'default') +
    '</span>';

  actions.innerHTML =
    '<button class="btn btn-sm" onclick="clearChat()" title="Clear conversation">&#x1F5D1; Clear</button>' +
    '<button class="btn btn-sm btn-danger" onclick="deleteAgent(\'' + escAttr(id) + '\')" title="Delete agent">&#x1F5D1; Delete</button>';
}

function clearChat() {
  if (state.activeAgentId) {
    state.conversations[state.activeAgentId] = [];
    renderChat();
  }
}

// ---------------------------------------------------------------------------
// Modal
// ---------------------------------------------------------------------------
function openNewAgentModal() {
  document.getElementById('newAgentModal').classList.add('open');
  document.getElementById('agentName').focus();
}
function closeNewAgentModal() {
  document.getElementById('newAgentModal').classList.remove('open');
}
document.getElementById('newAgentModal').addEventListener('click', function(e) {
  if (e.target === this) closeNewAgentModal();
});

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------
function escHtml(s) {
  if (!s) return '';
  const d = document.createElement('div');
  d.textContent = s;
  return d.innerHTML;
}
function escAttr(s) {
  if (!s) return '';
  return s.replace(/'/g, "\\'").replace(/"/g, '&quot;');
}
function formatDuration(ns) {
  if (!ns) return '';
  const ms = ns / 1000000;
  if (ms < 1000) return ms.toFixed(0) + 'ms';
  return (ms / 1000).toFixed(1) + 's';
}
function autoResize(el) {
  el.style.height = 'auto';
  el.style.height = Math.min(el.scrollHeight, 200) + 'px';
}

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------
document.addEventListener('DOMContentLoaded', async () => {
  const input = document.getElementById('userInput');
  input.addEventListener('input', () => autoResize(input));

  await Promise.all([loadAgents(), loadProviders(), loadModels()]);

  // Auto-select first agent if any
  if (state.agents.length > 0) {
    selectAgent(state.agents[0].id);
  }
});
</script>
</body>
</html>
`
