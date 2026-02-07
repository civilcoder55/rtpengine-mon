const state = {
    calls: [],
    activeSpy: null,
    audioContext: null,
    analyser: null,
    dataArray: null,
    animationFrame: null,
    currentCallDetails: null,
    currentView: 'stats',
    statsInterval: null
};

// --- API ---

function showView(view, navLink) {
    state.currentView = view;

    // Update Nav
    document.querySelectorAll('.nav-link').forEach(a => a.classList.remove('active'));
    if (navLink) navLink.classList.add('active');

    // Toggle View Containers
    const monitorView = document.getElementById('monitoring-view');
    const statsView = document.getElementById('stats-view');

    if (view === 'calls') {
        monitorView.classList.remove('hidden');
        statsView.classList.add('hidden');
        if (state.statsInterval) {
            clearInterval(state.statsInterval);
            state.statsInterval = null;
        }
        fetchCalls();
    } else {
        monitorView.classList.add('hidden');
        statsView.classList.remove('hidden');
        statsView.innerHTML = '<div class="mono" style="padding: 2rem; color: var(--text-secondary)">Loading statistics...</div>';
        if (state.statsInterval) {
            clearInterval(state.statsInterval);
        }
        fetchStats();
        state.statsInterval = setInterval(fetchStats, 5000);
    }
}

async function fetchStats() {
    if (state.currentView !== 'stats') return;
    try {
        const res = await fetch('/stats');
        if (!res.ok) throw new Error('Failed to fetch stats');
        const data = await res.json();

        const statusEl = document.getElementById('connection-status');
        if (statusEl) {
            statusEl.textContent = 'Connected';
            statusEl.style.color = 'var(--accent)';
        }

        renderStats(data.statistics || data || {});
    } catch (err) {
        console.error(err);
        const statusEl = document.getElementById('connection-status');
        if (statusEl) {
            statusEl.textContent = 'Disconnected';
            statusEl.style.color = 'var(--danger)';
        }
    }
}

function renderStats(stats) {
    const container = document.getElementById('stats-view');
    // The JSON response uses non-spaced keys: currentstatistics, totalstatistics, controlstatistics
    const curr = stats.currentstatistics || {};
    const total = stats.totalstatistics || {};
    const control = stats.controlstatistics || {};

    const fmt = (n) => typeof n === 'number' ? n.toLocaleString() : n || '0';
    const dur = (s) => {
        const sec = parseInt(s || '0', 10);
        if (sec < 60) return `${sec}s`;
        if (sec < 3600) return `${Math.floor(sec / 60)}m ${sec % 60}s`;
        return `${Math.floor(sec / 3600)}h ${Math.floor((sec % 3600) / 60)}m`;
    };

    container.innerHTML = `
        <div class="stats-grid">
            <div class="card">
                <div class="card-header"><h2>LIVE TRAFFIC</h2></div>
                <div class="card-content">
                    <div class="stat-row"><span class="stat-label">Active Sessions</span><span class="stat-val highlight">${fmt(curr.sessionsown)}</span></div>
                    <div class="stat-row"><span class="stat-label">Total Sessions</span><span class="stat-val">${fmt(curr.sessionstotal)}</span></div>
                    <div class="stat-row"><span class="stat-label">Packet Rate</span><span class="stat-val">${fmt(curr.packetrate)} pkts/s</span></div>
                    <div class="stat-row"><span class="stat-label">Byte Rate</span><span class="stat-val">${fmt(curr.byterate)} bytes/s</span></div>
                </div>
            </div>
            <div class="card">
                <div class="card-header"><h2>SYSTEM STATUS</h2></div>
                <div class="card-content">
                    <div class="stat-row"><span class="stat-label">Uptime</span><span class="stat-val success">${dur(total.uptime)}</span></div>
                    <div class="stat-row"><span class="stat-label">Processed Sessions</span><span class="stat-val">${fmt(total.managedsessions)}</span></div>
                    <div class="stat-row"><span class="stat-label">Avg Call Duration</span><span class="stat-val">${parseFloat(total.avgcallduration || 0).toFixed(2)}s</span></div>
                </div>
            </div>
            <div class="card">
                <div class="card-header"><h2>HEALTH & ERRORS</h2></div>
                <div class="card-content">
                    <div class="stat-row"><span class="stat-label">Current Error Rate</span><span class="stat-val ${curr.errorrate > 0 ? 'error' : 'success'}">${fmt(curr.errorrate)}</span></div>
                    <div class="stat-row"><span class="stat-label">Relayed Packet Errors</span><span class="stat-val">${fmt(total.relayedpacketerrors)}</span></div>
                    <div class="stat-row"><span class="stat-label">Reject Sessions</span><span class="stat-val ${total.rejectedsessions > 0 ? 'error' : ''}">${fmt(total.rejectedsessions)}</span></div>
                </div>
            </div>
        </div>
    `;
}

async function fetchCalls() {
    try {
        const res = await fetch('/calls');
        if (!res.ok) throw new Error('Network response was not ok');
        const calls = await res.json();
        const newCallObjects = (calls || []).map(id => ({ id, status: 'Active' }));

        // Update connection status
        const statusEl = document.getElementById('connection-status');
        if (statusEl) {
            statusEl.textContent = 'Connected';
            statusEl.style.color = 'var(--accent)';
        }

        // Check if changed
        const hasChanged = JSON.stringify(state.calls) !== JSON.stringify(newCallObjects);
        state.calls = newCallObjects;

        if (hasChanged || state.calls.length === 0) {
            renderCalls();
        }
    } catch (err) {
        console.error("Failed to fetch calls:", err);
        const statusEl = document.getElementById('connection-status');
        if (statusEl) {
            statusEl.textContent = 'Disconnected';
            statusEl.style.color = 'var(--danger)';
        }
    }
}

async function startSpying(callID) {
    if (state.activeSpy) {
        stopSpy();
    }

    logToTerminal(`Starting spy session for: ${callID.substring(0, 12)}...`);
    updateStreamStatus('Connecting...', false);

    // Show Spy Modal
    const spyOverlay = document.getElementById('spy-overlay');
    const spyCallId = document.getElementById('spy-call-id');
    if (spyCallId) spyCallId.textContent = callID;
    if (spyOverlay) spyOverlay.classList.remove('hidden');

    const pc = new RTCPeerConnection({ iceServers: [] });

    pc.onconnectionstatechange = () => {
        logToTerminal(`Connection State: ${pc.connectionState}`);
        if (pc.connectionState === 'connected') {
            updateStreamStatus('Streaming active', true);
        } else if (pc.connectionState === 'failed' || pc.connectionState === 'closed') {
            updateStreamStatus('No active stream', false);
        }
    };

    pc.ontrack = (event) => {
        logToTerminal(`Audio track received`);
        const audio = document.getElementById('remoteAudio');
        const stream = event.streams[0] || new MediaStream([event.track]);
        audio.srcObject = stream;
    };

    try {
        const res = await fetch(`/spy/${callID}`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ from_tag: "", to_tag: "" })
        });

        if (!res.ok) throw new Error(await res.text());

        const { spyID, sdp } = await res.json();
        logToTerminal(`Spy session created: ${spyID}`);

        await pc.setRemoteDescription({ type: 'offer', sdp });
        const answer = await pc.createAnswer();
        await pc.setLocalDescription(answer);

        const ansRes = await fetch(`/spy/answer/${spyID}`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ sdp: pc.localDescription.sdp })
        });

        if (!ansRes.ok) throw new Error(await ansRes.text());

        logToTerminal("Spying handshake complete");
        state.activeSpy = { pc, id: spyID };
    } catch (err) {
        logToTerminal(`Error: ${err.message}`);
        updateStreamStatus('No active stream', false);
        pc.close();
    }
}

function stopSpy() {
    if (state.activeSpy) {
        logToTerminal(`Stopping spy session...`);
        state.activeSpy.pc.close();
        state.activeSpy = null;
    }
    const audio = document.getElementById('remoteAudio');
    if (audio) audio.srcObject = null;
    updateStreamStatus('No active stream', false);

    // Hide Spy Modal
    const spyOverlay = document.getElementById('spy-overlay');
    if (spyOverlay) spyOverlay.classList.add('hidden');
}

// --- UI Rendering ---

function renderCalls() {
    const container = document.getElementById('calls-list');
    if (!container) return;

    if (state.calls.length === 0) {
        container.innerHTML = `
            <div class="empty-state">
                <div class="empty-icon">
                    <svg xmlns="http://www.w3.org/2000/svg" width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m11 20 2-2 5-5"/><path d="M22 21v-2a4 4 0 0 0-3-3.87"/><path d="M3 21v-2a4 4 0 0 1 4-4h5a4 4 0 0 1 4 4v2"/><path d="M16 11.37A4 4 0 1 1 12.63 8"/><path d="M25 21l-3-3"/></svg>
                </div>
                <p>No active calls</p>
            </div>
        `;
        return;
    }

    let rowsHtml = state.calls.map(call => `
        <tr>
            <td class="mono">${call.id.substring(0, 24)}...</td>
            <td><span class="status-badge">Active</span></td>
            <td style="text-align: right;">
                <button class="btn-primary" onclick="startSpying('${call.id}')">Spy</button>
                <button class="btn-text" style="display:inline-block; margin-left: 10px;" onclick="viewDetails('${call.id}')">Details</button>
            </td>
        </tr>
    `).join('');

    container.innerHTML = `
        <table class="data-table">
            <thead>
                <tr>
                    <th>CALL ID</th>
                    <th>STATUS</th>
                    <th style="text-align: right;">ACTIONS</th>
                </tr>
            </thead>
            <tbody>${rowsHtml}</tbody>
        </table>
    `;
}

function updateStreamStatus(text, isActive) {
    const indicator = document.getElementById('stream-status-indicator');
    const textEl = document.getElementById('stream-status-text');
    if (indicator && textEl) {
        textEl.textContent = text;
        if (isActive) {
            indicator.classList.add('active');
        } else {
            indicator.classList.remove('active');
        }
    }
}

function logToTerminal(msg) {
    const log = document.getElementById('activity-log');
    if (!log) return;

    if (log.innerHTML.includes('Waiting for activity...')) {
        log.innerHTML = '';
    }

    const entry = document.createElement('div');
    entry.className = 'log-entry';
    const time = new Date().toTimeString().split(' ')[0];
    entry.innerHTML = `
        <span class="log-time">${time}</span>
        <span class="log-msg">${msg}</span>
    `;
    log.appendChild(entry);
    log.scrollTop = log.scrollHeight;
}

function clearLogs() {
    const log = document.getElementById('activity-log');
    if (log) {
        log.innerHTML = `
            <div class="log-entry">
                <span class="log-time">--:--:--</span>
                <span class="log-msg">Waiting for activity...</span>
            </div>
        `;
    }
}

// --- Details Modal Logic ---

async function viewDetails(id) {
    const overlay = document.getElementById('details-overlay');
    const title = document.getElementById('modal-call-id');
    const content = document.getElementById('modal-content');

    title.textContent = id;
    content.innerHTML = '<div class="mono" style="color:var(--text-secondary)">Loading...</div>';
    overlay.classList.remove('hidden');

    try {
        const res = await fetch(`/calls/${id}`);
        const data = await res.json();
        state.currentCallDetails = data;
        renderTab('json');
    } catch (e) {
        content.innerHTML = '<div style="color:var(--danger)">Error loading details</div>';
    }
}

function closeDetails() {
    document.getElementById('details-overlay').classList.add('hidden');
}

function renderTab(tab) {
    const content = document.getElementById('modal-content');
    const data = state.currentCallDetails;

    if (tab === 'json') {
        content.innerHTML = `<pre class="json-tree">${syntaxHighlight(data)}</pre>`;
    } else {
        let html = `<div class="structured-view">`;

        // 1. General Info Section
        html += `
            <div class="view-section">
                <h4>General Information</h4>
                <div class="info-grid">
                    <div class="info-item">
                        <span class="info-label">Result</span>
                        <span class="info-value">${data.result || 'N/A'}</span>
                    </div>
                    <div class="info-item">
                        <span class="info-label">Created At</span>
                        <span class="info-value">${data.created ? new Date(data.created * 1000).toLocaleString() : 'N/A'}</span>
                    </div>
                     <div class="info-item">
                        <span class="info-label">Last Signal</span>
                        <span class="info-value">${data['last signal'] ? new Date(data['last signal'] * 1000).toLocaleTimeString() : 'N/A'}</span>
                    </div>
                </div>
            </div>
        `;

        // 2. Tags & Medias Section
        if (data.tags) {
            html += `<div class="view-section"><h4>Call Tags & Media</h4>`;
            for (const [tagName, tagData] of Object.entries(data.tags)) {
                html += `
                    <div style="margin-bottom: 1.5rem;">
                        <div style="font-size: 0.75rem; font-weight: 700; color: var(--text-secondary); margin-bottom: 0.75rem; display: flex; align-items: center; gap: 0.5rem;">
                            <span style="color: var(--brand)">TAG:</span> ${tagName}
                        </div>
                        <div class="mini-table-wrapper">
                            <table class="mini-table">
                                <thead>
                                    <tr>
                                        <th>Type</th>
                                        <th>Protocol</th>
                                        <th>Address & Port</th>
                                        <th>Rx/Tx Pkts</th>
                                    </tr>
                                </thead>
                                <tbody>
                `;

                if (tagData.medias) {
                    tagData.medias.forEach(media => {
                        const stream = (media.streams && media.streams[0]) || {};
                        const ep = stream.endpoint || {};
                        const statsIn = stream.stats ? stream.stats.packets : 0;
                        const statsOut = stream.stats_out ? stream.stats_out.packets : 0;

                        html += `
                            <tr>
                                <td style="text-transform: capitalize;">${media.type}</td>
                                <td>${media.protocol}</td>
                                <td class="mono">${ep.address || 'N/A'}:${ep.port || 'N/A'}</td>
                                <td class="mono">${statsIn} / ${statsOut}</td>
                            </tr>
                        `;
                    });
                } else {
                    html += `<tr><td colspan="4" style="text-align: center; color: var(--text-muted);">No media streams active for this tag</td></tr>`;
                }

                html += `</tbody></table></div></div>`;
            }
            html += `</div>`;
        }

        // 3. Totals Section
        if (data.totals) {
            let pktsRx = data.totals.packets || 0;
            let bytesRx = data.totals.bytes || 0;

            if (data.totals.RTP) {
                pktsRx += data.totals.RTP.packets || 0;
                bytesRx += data.totals.RTP.bytes || 0;
            }
            if (data.totals.RTCP) {
                pktsRx += data.totals.RTCP.packets || 0;
                bytesRx += data.totals.RTCP.bytes || 0;
            }

            if (pktsRx === 0 && data.tags) {
                Object.values(data.tags).forEach(tag => {
                    if (tag.medias) {
                        tag.medias.forEach(m => {
                            if (m.streams) {
                                m.streams.forEach(s => {
                                    pktsRx += (s.stats && s.stats.packets) || 0;
                                    bytesRx += (s.stats && s.stats.bytes) || 0;
                                });
                            }
                        });
                    }
                });
            }

            html += `
                <div class="view-section">
                    <h4>Cumulative Totals</h4>
                    <div class="info-grid">
                        <div class="info-item">
                            <span class="info-label">Total Rx Packets</span>
                            <span class="info-value">${pktsRx.toLocaleString()}</span>
                        </div>
                        <div class="info-item">
                            <span class="info-label">Total Rx Data</span>
                            <span class="info-value">${(bytesRx / 1024).toFixed(2)} KB</span>
                        </div>
                    </div>
                </div>
            `;
        }

        html += `</div>`;
        content.innerHTML = html;
    }

    document.querySelectorAll('.tab-btn').forEach(btn => {
        btn.classList.toggle('active', btn.dataset.tab === tab);
    });
}


document.addEventListener('click', (e) => {
    if (e.target.classList.contains('tab-btn')) {
        renderTab(e.target.dataset.tab);
    }
});

function syntaxHighlight(json) {
    if (typeof json != 'string') {
        json = JSON.stringify(json, undefined, 2);
    }
    json = json.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
    return json.replace(/("(\\u[a-zA-Z0-9]{4}|\\[^u]|[^\\"])*"(\s*:)?|\b(true|false|null)\b|-?\d+(?:\.\d*)?(?:[eE][+\-]?\d+)?)/g, function (match) {
        var cls = 'json-number';
        if (/^"/.test(match)) {
            if (/:$/.test(match)) {
                cls = 'json-key';
            } else {
                cls = 'json-string';
            }
        } else if (/true|false/.test(match)) {
            cls = 'json-boolean';
        } else if (/null/.test(match)) {
            cls = 'json-null';
        }
        return '<span class="' + cls + '">' + match + '</span>';
    });
}

// Init
showView('stats', document.querySelector('.nav-link[onclick*="stats"]'));
