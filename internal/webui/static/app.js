const state = {
  token: sessionStorage.getItem("nvr_admin_token") || "",
  principal: null,
  cameras: {},
  statuses: {},
  hls: null,
  webrtc: null,
  webrtcSession: "",
  playerCamera: null,
  onvifProfiles: [],
  mosaicHls: [],
  mosaicSize: 4
};

const byId = function (id) { return document.getElementById(id); };

function escapeHTML(value) {
  return String(value == null ? "" : value).replace(/[&<>"']/g, function (c) {
    return {"&":"&amp;","<":"&lt;",">":"&gt;","\"":"&quot;","'":"&#039;"}[c];
  });
}

function notice(text, bad) {
  byId("notice").innerHTML = text
    ? '<div class="notice ' + (bad ? "bad" : "") + '">' + escapeHTML(text) + "</div>"
    : "";
}

async function api(path, options) {
  options = options || {};
  const headers = Object.assign({}, options.headers || {});
  if (state.token) headers.Authorization = "Bearer " + state.token;
  if (options.body) headers["Content-Type"] = "application/json";
  const response = await fetch(path, Object.assign({}, options, {headers: headers}));
  if (response.status === 204) return null;
  const data = await response.json().catch(function () { return {}; });
  if (!response.ok) {
    const err = new Error(data.error || ("HTTP " + response.status));
    err.status = response.status;
    err.data = data;
    throw err;
  }
  return data;
}

function can(permission) {
  if (!state.principal) return false;
  const role = state.principal.role;
  if (role === "admin") return true;
  if (permission === "view") return ["viewer","operator","supervisor"].indexOf(role) >= 0;
  if (permission === "operate") return ["operator","supervisor"].indexOf(role) >= 0;
  if (permission === "evidence") return role === "supervisor";
  return false;
}

function updateIdentityUI() {
  const p = state.principal;
  byId("currentUser").textContent = p ? (p.username + " · " + p.role) : "";
  byId("authBtn").textContent = p ? "Sair" : "Entrar";
  byId("adminBtn").hidden = !(p && p.role === "admin");
  byId("discoverBtn").hidden = !can("admin");
  byId("addBtn").hidden = !can("admin");
}

async function authenticateToken(token) {
  state.token = token;
  sessionStorage.setItem("nvr_admin_token", token);
  try {
    state.principal = await api("/api/v1/auth/me");
    updateIdentityUI();
    await loadCameras();
    return true;
  } catch (err) {
    state.token = "";
    state.principal = null;
    sessionStorage.removeItem("nvr_admin_token");
    updateIdentityUI();
    throw err;
  }
}

async function health() {
  try {
    const response = await fetch("/api/v1/health");
    if (!response.ok) throw new Error("offline");
    byId("health").textContent = "Core online";
    byId("health").classList.add("ok");
  } catch (_) {
    byId("health").textContent = "Core offline";
    byId("health").classList.remove("ok");
  }
}

function bytesHuman(value) {
  let n = Number(value || 0);
  const units = ["B","KB","MB","GB","TB","PB"];
  let i = 0;
  while (n >= 1024 && i < units.length - 1) { n /= 1024; i++; }
  return (i === 0 ? n.toFixed(0) : n.toFixed(1)) + " " + units[i];
}

function mediaBadge(status, enabled) {
  if (!enabled) return '<span class="pill">desabilitada</span>';
  if (!status) return '<span class="pill">iniciando</span>';
  const good = status.state === "recording";
  const label = escapeHTML(status.state || "stopped");
  const codec = status.codec ? " · " + escapeHTML(status.codec) : "";
  return '<span class="pill ' + (good ? "ok" : (status.state === "error" ? "bad-pill" : "")) + '">' + label + codec + "</span>" +
    (status.last_error ? '<div class="muted small media-error">' + escapeHTML(status.last_error) + "</div>" : "");
}

async function loadCameras() {
  if (!state.token) return;
  try {
    const results = await Promise.all([
      api("/api/v1/cameras"),
      api("/api/v1/media/status"),
      api("/api/v1/system/status")
    ]);
    const data = results[0];
    const media = results[1];
    const system = results[2];
    const statuses = {};
    (media.items || []).forEach(function (s) { statuses[s.camera_id] = s; });

    state.cameras = {};
    state.statuses = statuses;
    (data.items || []).forEach(function (camera) { state.cameras[camera.id] = camera; });

    byId("cameraCount").textContent = data.count;
    byId("enabledCount").textContent = data.items.filter(function (x) { return x.enabled; }).length;
    byId("recordingCount").textContent = Object.values(statuses).filter(function (x) { return x.state === "recording"; }).length;
    byId("diskFree").textContent = bytesHuman(system.disk_free_bytes);

    if (!data.items.length) {
      byId("cameraRows").innerHTML = '<tr><td colspan="5" class="empty">Nenhuma câmera cadastrada.</td></tr>';
      return;
    }

    byId("cameraRows").innerHTML = data.items.map(function (c) {
      const snapButton = c.snapshot_url
        ? '<button class="secondary snap" data-id="' + c.id + '">Snapshot</button> '
        : "";
      const syncButton = c.onvif_url && can("admin")
        ? '<button class="secondary sync-onvif" data-id="' + c.id + '">Sync ONVIF</button> '
        : "";
      const testButton = can("operate")
        ? '<button class="secondary test" data-id="' + c.id + '">Testar</button> '
        : "";
      const deleteButton = can("admin")
        ? '<button class="danger del" data-id="' + c.id + '">Excluir</button>'
        : "";
      const origin = c.onvif_url
        ? '<span class="pill ok">ONVIF</span><div class="muted small">' + escapeHTML(c.onvif_profile_token || "") + '</div>'
        : '<span class="pill">RTSP manual</span>';
      return "<tr>" +
        "<td><strong>" + escapeHTML(c.name) + "</strong><div class=\"muted small\">" + escapeHTML(c.description || "") + "</div></td>" +
        "<td>" + escapeHTML(c.city || "—") + "<div class=\"muted small\">" + escapeHTML(c.site || "") + "</div></td>" +
        "<td>" + origin + "<div class=\"muted small source-url\">" + escapeHTML(c.rtsp_url) + "</div></td>" +
        "<td>" + mediaBadge(statuses[c.id], c.enabled) + "</td>" +
        "<td class=\"row-actions\"><button class=\"live\" data-id=\"" + c.id + "\">Ao vivo</button> " +
        "<button class=\"play\" data-id=\"" + c.id + "\">Playback</button> " +
        testButton + snapButton + syncButton + deleteButton + "</td></tr>";
    }).join("");
  } catch (e) {
    notice(e.message, true);
    if (e.status === 401) {
      state.token = "";
      state.principal = null;
      sessionStorage.removeItem("nvr_admin_token");
      updateIdentityUI();
      byId("tokenDialog").showModal();
    }
  }
}

async function showSnapshot(id) {
  byId("snapshotImage").hidden = true;
  byId("snapshotLoading").hidden = false;
  byId("snapshotLoading").textContent = "Carregando...";
  byId("snapshotDialog").showModal();
  try {
    const response = await fetch("/api/v1/cameras/" + id + "/snapshot", {
      headers: {Authorization: "Bearer " + state.token}
    });
    if (!response.ok) {
      const data = await response.json().catch(function () { return {}; });
      throw new Error(data.error || ("HTTP " + response.status));
    }
    const blob = await response.blob();
    const url = URL.createObjectURL(blob);
    const image = byId("snapshotImage");
    image.onload = function () { URL.revokeObjectURL(url); };
    image.src = url;
    image.hidden = false;
    byId("snapshotLoading").hidden = true;
  } catch (err) {
    byId("snapshotLoading").textContent = err.message;
  }
}

function releaseWebRTC() {
  const session = state.webrtcSession;
  state.webrtcSession = "";
  if (session && state.token) {
    fetch("/api/v1/webrtc/sessions/" + encodeURIComponent(session), {
      method: "DELETE",
      headers: {Authorization: "Bearer " + state.token},
      keepalive: true
    }).catch(function () {});
  }
  if (state.webrtc) {
    try { state.webrtc.close(); } catch (_) {}
    state.webrtc = null;
  }
  const video = byId("playerVideo");
  video.srcObject = null;
}

function destroyPlayback() {
  releaseWebRTC();
  if (state.hls) {
    state.hls.destroy();
    state.hls = null;
  }
  const video = byId("playerVideo");
  video.pause();
  video.removeAttribute("src");
  video.srcObject = null;
  video.load();
}

function attachHLS(playlist, label, isLive) {
  const video = byId("playerVideo");
  if (video.canPlayType("application/vnd.apple.mpegurl")) {
    video.src = playlist;
    byId("playerStatus").textContent = label + " · HLS nativo";
    video.play().catch(function () {});
    return true;
  }
  if (window.Hls && window.Hls.isSupported()) {
    state.hls = new window.Hls({
      enableWorker: true,
      lowLatencyMode: false,
      backBufferLength: isLive ? 20 : 60,
      liveSyncDurationCount: isLive ? 3 : undefined,
      liveMaxLatencyDurationCount: isLive ? 6 : undefined
    });
    state.hls.loadSource(playlist);
    state.hls.attachMedia(video);
    state.hls.on(window.Hls.Events.MANIFEST_PARSED, function () {
      byId("playerStatus").textContent = label;
      video.play().catch(function () {});
    });
    state.hls.on(window.Hls.Events.ERROR, function (_event, data) {
      if (data && data.fatal) byId("playerStatus").textContent = "Falha HLS: " + (data.details || data.type || "erro");
    });
    return true;
  }
  byId("playerStatus").textContent = "Este navegador não possui HLS/MSE compatível.";
  return false;
}

function waitForICEGatheringComplete(pc, timeoutMs) {
  if (pc.iceGatheringState === "complete") return Promise.resolve();
  return new Promise(function (resolve) {
    let finished = false;
    const finish = function () {
      if (finished) return;
      finished = true;
      pc.removeEventListener("icegatheringstatechange", onChange);
      resolve();
    };
    const onChange = function () { if (pc.iceGatheringState === "complete") finish(); };
    pc.addEventListener("icegatheringstatechange", onChange);
    setTimeout(finish, timeoutMs || 5000);
  });
}

async function startHLSLive(id, reason) {
  if (reason) byId("playerStatus").textContent = reason + " Usando HLS...";
  const session = await api("/api/v1/cameras/" + id + "/live/session", {
    method:"POST",
    body:JSON.stringify({ttl_seconds:28800})
  });
  attachHLS(session.playlist_url, "Ao vivo HLS local", true);
}

async function startWebRTC(id) {
  if (!window.RTCPeerConnection) throw new Error("WebRTC indisponível neste navegador.");
  const pc = new RTCPeerConnection({iceServers: []});
  state.webrtc = pc;
  let fallbackStarted = false;
  pc.addTransceiver("video", {direction:"recvonly"});
  pc.ontrack = function (event) {
    const video = byId("playerVideo");
    video.srcObject = event.streams && event.streams[0] ? event.streams[0] : new MediaStream([event.track]);
    video.play().catch(function () {});
  };
  pc.onconnectionstatechange = function () {
    if (state.webrtc !== pc) return;
    if (pc.connectionState === "connected") {
      byId("playerStatus").textContent = "Ao vivo WebRTC · baixa latência";
      return;
    }
    if (pc.connectionState === "failed" && !fallbackStarted) {
      fallbackStarted = true;
      releaseWebRTC();
      startHLSLive(id, "WebRTC perdeu a conexão.").catch(function (err) { byId("playerStatus").textContent = err.message; });
    }
  };
  try {
    const offer = await pc.createOffer();
    await pc.setLocalDescription(offer);
    await waitForICEGatheringComplete(pc, 5000);
    if (!pc.localDescription) throw new Error("Oferta WebRTC indisponível.");
    const answer = await api("/api/v1/cameras/" + id + "/webrtc/session", {
      method:"POST",
      body:JSON.stringify({type:"offer",sdp:pc.localDescription.sdp})
    });
    state.webrtcSession = answer.session_id || "";
    await pc.setRemoteDescription({type:"answer", sdp:answer.sdp});
    byId("playerStatus").textContent = "Negociando ICE/DTLS...";
  } catch (err) {
    releaseWebRTC();
    throw err;
  }
}

function inputDate(date) {
  const d = new Date(date);
  const pad = function (v) { return String(v).padStart(2,"0"); };
  return d.getFullYear() + "-" + pad(d.getMonth()+1) + "-" + pad(d.getDate()) + "T" + pad(d.getHours()) + ":" + pad(d.getMinutes()) + ":" + pad(d.getSeconds());
}

function timelineRange() {
  const from = byId("timelineFrom").value;
  const to = byId("timelineTo").value;
  if (!from || !to) throw new Error("Informe início e fim.");
  const a = new Date(from);
  const b = new Date(to);
  if (!(b > a)) throw new Error("O fim deve ser posterior ao início.");
  return {from:a.toISOString(),to:b.toISOString()};
}

async function loadTimelineVisual() {
  if (!state.playerCamera) return;
  const range = timelineRange();
  const result = await api("/api/v1/cameras/" + state.playerCamera + "/timeline?from=" + encodeURIComponent(range.from) + "&to=" + encodeURIComponent(range.to) + "&limit=1000");
  const bar = byId("timelineBar");
  if (!result.count) {
    bar.innerHTML = '<div class="empty compact">Sem gravação no intervalo.</div>';
    byId("timelineInfo").textContent = "";
    return result;
  }
  bar.innerHTML = result.items.map(function (segment) {
    const duration = Math.max(0, (new Date(segment.end) - new Date(segment.start)) / 1000);
    return '<button class="timeline-segment ' + (segment.protected ? "protected" : "") + '" data-start="' + escapeHTML(segment.start) + '" data-end="' + escapeHTML(segment.end) + '" title="' + escapeHTML(new Date(segment.start).toLocaleString()) + '">' + Math.round(duration) + "s</button>";
  }).join("");
  byId("timelineInfo").textContent = result.count + " segmento(s) no intervalo.";
  return result;
}

async function startPlaybackRange() {
  const range = timelineRange();
  destroyPlayback();
  const session = await api("/api/v1/cameras/" + state.playerCamera + "/playback/session", {
    method:"POST",
    body:JSON.stringify({from:range.from,to:range.to,limit:500,ttl_seconds:900})
  });
  attachHLS(session.playlist_url, "Playback HLS local", false);
}

async function showLive(id) {
  const camera = state.cameras[id];
  if (!camera) return;
  state.playerCamera = id;
  byId("playerTitle").textContent = camera.name + " — Ao vivo";
  byId("playerStatus").textContent = "Tentando WebRTC de baixa latência...";
  byId("timelineControls").hidden = true;
  byId("ptzPanel").hidden = !(camera.onvif_ptz && can("operate"));
  byId("playerDialog").showModal();
  destroyPlayback();
  try {
    await startWebRTC(id);
  } catch (err) {
    try {
      const reason = err && err.data && err.data.codec
        ? ("WebRTC sem transcodificação não suporta " + err.data.codec + ".")
        : ("WebRTC indisponível: " + err.message + ".");
      await startHLSLive(id, reason);
    } catch (fallbackErr) {
      byId("playerStatus").textContent = fallbackErr.message;
    }
  }
}

async function showPlayback(id) {
  const camera = state.cameras[id];
  if (!camera) return;
  state.playerCamera = id;
  byId("playerTitle").textContent = camera.name + " — Playback";
  byId("playerStatus").textContent = "Selecione o intervalo.";
  byId("timelineControls").hidden = false;
  byId("ptzPanel").hidden = true;
  const now = new Date();
  byId("timelineTo").value = inputDate(now);
  byId("timelineFrom").value = inputDate(new Date(now.getTime() - 60*60*1000));
  byId("exportEvidence").hidden = !can("evidence");
  byId("playerDialog").showModal();
  destroyPlayback();
  try {
    const result = await loadTimelineVisual();
    if (result && result.count) await startPlaybackRange();
  } catch (err) {
    byId("playerStatus").textContent = err.message;
  }
}

function destroyMosaic() {
  state.mosaicHls.forEach(function (h) { try { h.destroy(); } catch (_) {} });
  state.mosaicHls = [];
  byId("mosaicGrid").querySelectorAll("video").forEach(function (video) {
    video.pause();
    video.removeAttribute("src");
    video.load();
  });
}

async function attachMosaicTile(video, id, statusEl) {
  try {
    const session = await api("/api/v1/cameras/" + id + "/live/session", {
      method:"POST",
      body:JSON.stringify({ttl_seconds:3600})
    });
    if (video.canPlayType("application/vnd.apple.mpegurl")) {
      video.src = session.playlist_url;
      video.play().catch(function () {});
      return;
    }
    if (window.Hls && window.Hls.isSupported()) {
      const h = new window.Hls({enableWorker:true,lowLatencyMode:false,backBufferLength:10,liveSyncDurationCount:3});
      state.mosaicHls.push(h);
      h.loadSource(session.playlist_url);
      h.attachMedia(video);
      h.on(window.Hls.Events.MANIFEST_PARSED,function(){ video.play().catch(function(){}); });
      h.on(window.Hls.Events.ERROR,function(_e,data){ if(data && data.fatal) statusEl.textContent="Falha HLS"; });
      return;
    }
    statusEl.textContent = "HLS incompatível";
  } catch (err) {
    statusEl.textContent = err.message;
  }
}

async function renderMosaic(size) {
  state.mosaicSize = size || state.mosaicSize;
  destroyMosaic();
  const grid = byId("mosaicGrid");
  grid.className = "mosaic-grid grid-" + state.mosaicSize;
  const cameras = Object.values(state.cameras).filter(function (c) { return c.enabled; }).slice(0,state.mosaicSize);
  if (!cameras.length) {
    grid.innerHTML = '<div class="empty">Nenhuma câmera habilitada.</div>';
    return;
  }
  grid.innerHTML = cameras.map(function(c){
    return '<article class="mosaic-tile"><div class="mosaic-title"><strong>' + escapeHTML(c.name) + '</strong><span class="muted small">' + escapeHTML(c.site || c.city || "") + '</span></div><video data-camera="' + c.id + '" muted autoplay playsinline></video><div class="mosaic-status muted small"></div></article>';
  }).join("");
  grid.querySelectorAll("video").forEach(function(video){
    const statusEl = video.parentElement.querySelector(".mosaic-status");
    attachMosaicTile(video,video.dataset.camera,statusEl);
    video.onclick = function(){ showLive(video.dataset.camera); };
  });
}

async function loadSystem() {
  const results = await Promise.all([
    api("/api/v1/system/status"),
    api("/api/v1/plugins/plate-ocr/status").catch(function(err){return {state:"offline",error:err.message};})
  ]);
  const data = results[0];
  const plate = results[1] || {};
  byId("systemCards").innerHTML = [
    ["Uptime",Math.floor(data.uptime_seconds/3600)+" h"],
    ["Plate OCR",plate.status || plate.state || "offline"],
    ["RAM Go",bytesHuman(data.memory_alloc_bytes)],
    ["Disco livre",bytesHuman(data.disk_free_bytes)],
    ["Uso do disco",Number(data.disk_used_percent||0).toFixed(1)+"%"],
    ["Gravando",data.cameras_recording+"/"+data.cameras_total],
    ["Erros",data.cameras_error],
    ["Reconnects",data.reconnects],
    ["Goroutines",data.goroutines]
  ].map(function(x){return '<article><span>'+escapeHTML(x[0])+'</span><strong>'+escapeHTML(x[1])+'</strong></article>';}).join("");
  byId("systemRaw").textContent = JSON.stringify({nvr:data,plate_ocr:plate},null,2);
}

async function showSystem() {
  byId("systemDialog").showModal();
  try { await loadSystem(); } catch(err) { byId("systemRaw").textContent=err.message; }
}

function renderONVIFDevices(items) {
  const target = byId("onvifDevices");
  if (!items || !items.length) {
    target.innerHTML = '<div class="empty compact">Nenhuma câmera ONVIF respondeu ao discovery.</div>';
    return;
  }
  target.innerHTML = items.map(function (device) {
    const xaddr = (device.xaddrs || [])[0] || "";
    const scopeName = (device.scopes || []).find(function (s) { return s.indexOf("/name/") >= 0; }) || "";
    const name = scopeName ? decodeURIComponent(scopeName.split("/name/").pop()) : xaddr;
    return '<button type="button" class="device-card" data-xaddr="' + escapeHTML(xaddr) + '">' +
      '<strong>' + escapeHTML(name || "Câmera ONVIF") + '</strong><span>' + escapeHTML(xaddr) + '</span><small>' + escapeHTML(device.from || "") + '</small></button>';
  }).join("");
}

async function inspectONVIF() {
  const form = new FormData(byId("onvifForm"));
  const endpoint = String(form.get("endpoint") || "").trim();
  if (!endpoint) { byId("onvifDeviceInfo").textContent = "Informe ou descubra um endpoint."; return; }
  byId("onvifDeviceInfo").textContent = "Consultando...";
  byId("addOnvifCamera").disabled = true;
  try {
    const result = await api("/api/v1/onvif/inspect", {
      method:"POST",
      body:JSON.stringify({endpoint:endpoint,username:String(form.get("username")||""),password:String(form.get("password")||"")})
    });
    state.onvifProfiles = result.profiles || [];
    const select = byId("onvifProfile");
    select.innerHTML = state.onvifProfiles.map(function (profile,index) {
      const title = (profile.name || ("Perfil "+(index+1))) + " · " + (profile.encoding||"?") +
        (profile.width ? (" "+profile.width+"×"+profile.height) : "") +
        (profile.frame_rate_limit ? (" @"+profile.frame_rate_limit+"fps") : "") +
        (profile.ptz ? " · PTZ" : "");
      return '<option value="'+escapeHTML(profile.token)+'" data-version="'+profile.media_version+'">'+escapeHTML(title)+'</option>';
    }).join("");
    select.disabled = state.onvifProfiles.length === 0;
    byId("addOnvifCamera").disabled = state.onvifProfiles.length === 0;
    const device=result.device||{};
    byId("onvifDeviceInfo").textContent=[device.manufacturer,device.model,device.firmware_version].filter(Boolean).join(" · ") || (state.onvifProfiles.length+" perfil(is)");
    const nameInput=byId("onvifForm").querySelector('input[name="name"]');
    if(!nameInput.value && (device.model||device.manufacturer)) nameInput.value=[device.manufacturer,device.model].filter(Boolean).join(" ");
  } catch(err) { byId("onvifDeviceInfo").textContent=err.message; }
}

async function ptzAction(action) {
  const id=state.playerCamera;
  if(!id || !can("operate")) return;
  if(action==="stop") { try{await api("/api/v1/cameras/"+id+"/ptz/stop",{method:"POST",body:"{}"});}catch(_){} return; }
  const moves={up:{pan:0,tilt:0.6,zoom:0},down:{pan:0,tilt:-0.6,zoom:0},left:{pan:-0.6,tilt:0,zoom:0},right:{pan:0.6,tilt:0,zoom:0},zoomin:{pan:0,tilt:0,zoom:0.6},zoomout:{pan:0,tilt:0,zoom:-0.6}};
  const move=moves[action]; if(!move)return;
  try{await api("/api/v1/cameras/"+id+"/ptz/move",{method:"POST",body:JSON.stringify(Object.assign({timeout_ms:650},move))});}
  catch(err){byId("playerStatus").textContent="PTZ: "+err.message;}
}

async function loadUsers() {
  const result=await api("/api/v1/users");
  byId("usersList").innerHTML=(result.items||[]).map(function(u){
    return '<div class="admin-row" data-user="'+u.id+'"><div><strong>'+escapeHTML(u.username)+'</strong><div class="muted small">'+escapeHTML(u.display_name||"")+'</div></div>'+
      '<select class="user-role"><option '+(u.role==="viewer"?"selected":"")+' value="viewer">Viewer</option><option '+(u.role==="operator"?"selected":"")+' value="operator">Operator</option><option '+(u.role==="supervisor"?"selected":"")+' value="supervisor">Supervisor</option><option '+(u.role==="admin"?"selected":"")+' value="admin">Admin</option></select>'+
      '<label class="check"><input class="user-enabled" type="checkbox" '+(u.enabled?"checked":"")+'> ativo</label>'+
      '<div class="toolbar"><button class="secondary user-save">Salvar</button><button class="secondary user-password">Senha</button><button class="danger user-delete">Excluir</button></div></div>';
  }).join("") || '<div class="empty compact">Nenhum usuário. Use o token bootstrap para criar o primeiro.</div>';
}

async function loadExports() {
  const result=await api("/api/v1/exports?limit=50");
  byId("exportsList").innerHTML=(result.items||[]).map(function(j){
    const action=j.status==="ready"?'<button class="secondary export-download" data-export="'+j.id+'">Download</button>':"";
    return '<div class="admin-row"><div><strong>'+escapeHTML(j.camera_name||j.camera_id)+'</strong><div class="muted small">'+new Date(j.requested_from).toLocaleString()+' → '+new Date(j.requested_to).toLocaleString()+'</div></div><span class="pill '+(j.status==="ready"?"ok":"")+'">'+escapeHTML(j.status)+' · '+j.progress+'%</span><div>'+action+'</div></div>';
  }).join("") || '<div class="empty compact">Nenhuma evidência exportada.</div>';
}

async function loadAudit() {
  const result=await api("/api/v1/audit?limit=100");
  byId("auditList").innerHTML=(result.items||[]).map(function(e){
    return '<div class="audit-row"><span>'+new Date(e.time).toLocaleString()+'</span><strong>'+escapeHTML(e.actor||"")+'</strong><code>'+escapeHTML(e.action)+'</code><span>'+escapeHTML(e.resource_id||e.resource||"")+'</span><span class="'+(e.success?"ok-text":"bad-text")+'">'+(e.success?"OK":"FALHA")+'</span></div>';
  }).join("") || '<div class="empty compact">Sem eventos.</div>';
}

async function loadAdmin() {
  await Promise.all([loadUsers(),loadExports(),loadAudit()]);
}

async function downloadExport(id) {
  const response=await fetch("/api/v1/exports/"+encodeURIComponent(id)+"/download",{headers:{Authorization:"Bearer "+state.token}});
  if(!response.ok){const d=await response.json().catch(function(){return{};});throw new Error(d.error||("HTTP "+response.status));}
  const blob=await response.blob();
  const url=URL.createObjectURL(blob);
  const a=document.createElement("a"); a.href=url; a.download="nvr-evidence-"+id+".tar.gz"; document.body.appendChild(a); a.click(); a.remove();
  setTimeout(function(){URL.revokeObjectURL(url);},1000);
}

function populatePlateCameraFilter() {
  const select=byId("plateSearchForm").querySelector('select[name="camera_id"]');
  const current=select.value;
  select.innerHTML='<option value="">Todas as câmeras</option>'+Object.values(state.cameras).map(function(cam){
    return '<option value="'+escapeHTML(cam.id)+'">'+escapeHTML(cam.name)+'</option>';
  }).join("");
  select.value=current;
}

async function searchPlates() {
  const form=new FormData(byId("plateSearchForm"));
  const q=new URLSearchParams();
  const plate=String(form.get("plate")||"").trim();
  const camera=String(form.get("camera_id")||"").trim();
  if(plate)q.set("plate",plate);
  if(camera)q.set("camera_id",camera);
  if(form.get("alert_only")==="on")q.set("alert_only","true");
  q.set("limit","250");
  const result=await api("/api/v1/events/plates?"+q.toString());
  byId("plateSearchInfo").textContent=result.count+" passagem(ns) encontrada(s) · "+result.total_events+" evento(s) armazenado(s)";
  byId("plateEvents").innerHTML=(result.items||[]).map(function(ev){
    const attrs=ev.attributes||{};
    const plateText=attrs.normalized_text||attrs.raw_text||"—";
    const camera=state.cameras[ev.camera_id];
    const alert=ev.alert?'<span class="pill alert-pill">ALERTA</span> ':"";
    const label=ev.alert_label?'<span class="alert-label">'+escapeHTML(ev.alert_label)+'</span>':"";
    const evidence=ev.snapshot_ref?'<button class="secondary plate-evidence" data-event="'+escapeHTML(ev.event_id)+'">Foto</button>':"";
    return '<article class="plate-event '+(ev.alert?"plate-alert":"")+'"><div class="plate-main">'+alert+'<strong class="plate-number">'+escapeHTML(plateText)+'</strong>'+label+'</div>'+
      '<div class="muted small">'+escapeHTML(camera?camera.name:ev.camera_id)+' · '+new Date(ev.observed_at).toLocaleString()+'</div>'+
      '<div class="plate-meta"><span>conf. '+Math.round(Number(ev.confidence||0)*100)+'%</span><span>'+escapeHTML(attrs.direction||"")+'</span><span>'+escapeHTML(attrs.lane||"")+'</span>'+evidence+'</div></article>';
  }).join("")||'<div class="empty compact">Nenhuma passagem encontrada.</div>';
}

async function showPlateEvidence(eventID) {
  byId("snapshotImage").hidden=true;
  byId("snapshotLoading").hidden=false;
  byId("snapshotLoading").textContent="Carregando evidência...";
  byId("snapshotDialog").showModal();
  try{
    const response=await fetch("/api/v1/events/"+encodeURIComponent(eventID)+"/evidence",{headers:{Authorization:"Bearer "+state.token}});
    if(!response.ok){const d=await response.json().catch(function(){return{};});throw new Error(d.error||("HTTP "+response.status));}
    const blob=await response.blob();
    const url=URL.createObjectURL(blob);
    const image=byId("snapshotImage");
    image.onload=function(){URL.revokeObjectURL(url);};
    image.src=url;image.hidden=false;byId("snapshotLoading").hidden=true;
  }catch(err){byId("snapshotLoading").textContent=err.message;}
}

async function loadHotlist() {
  if(!can("evidence"))return;
  const result=await api("/api/v1/hotlist");
  byId("hotlistList").innerHTML=(result.items||[]).map(function(item){
    return '<div class="admin-row hotlist-row" data-hotlist="'+escapeHTML(item.id)+'"><div><strong class="plate-number">'+escapeHTML(item.plate)+'</strong><div class="muted small">'+escapeHTML(item.label||"")+'</div></div>'+
      '<label class="check"><input class="hotlist-enabled" type="checkbox" '+(item.enabled?"checked":"")+'> ativo</label>'+
      '<div class="toolbar"><button class="secondary hotlist-save">Salvar</button><button class="danger hotlist-delete">Excluir</button></div></div>';
  }).join("")||'<div class="empty compact">Hotlist vazia.</div>';
}

async function openPlates() {
  if(!state.token){byId("tokenDialog").showModal();return;}
  populatePlateCameraFilter();
  byId("hotlistSection").hidden=!can("evidence");
  byId("platesDialog").showModal();
  try{
    await searchPlates();
    if(can("evidence"))await loadHotlist();
  }catch(err){notice(err.message,true);}
}

byId("authBtn").onclick = async function () {
  if (state.principal) {
    try { await api("/api/v1/auth/logout",{method:"POST",body:"{}"}); } catch (_) {}
    state.token=""; state.principal=null; sessionStorage.removeItem("nvr_admin_token"); destroyMosaic(); updateIdentityUI(); notice("Sessão encerrada.",false);
    return;
  }
  byId("tokenDialog").showModal();
};
byId("platesBtn").onclick=openPlates;
byId("closePlates").onclick=function(){byId("platesDialog").close();};
byId("mosaicBtn").onclick = function(){ if(!state.token){byId("tokenDialog").showModal();return;} byId("mosaicPanel").hidden=false; renderMosaic(state.mosaicSize); };
byId("closeMosaic").onclick=function(){destroyMosaic();byId("mosaicPanel").hidden=true;};
byId("systemBtn").onclick=function(){ if(!state.token){byId("tokenDialog").showModal();return;} showSystem(); };
byId("adminBtn").onclick=async function(){byId("adminDialog").showModal();try{await loadAdmin();}catch(err){notice(err.message,true);}};
byId("addBtn").onclick = function () { (state.token ? byId("cameraDialog") : byId("tokenDialog")).showModal(); };
byId("discoverBtn").onclick = function () { (state.token ? byId("onvifDialog") : byId("tokenDialog")).showModal(); };
byId("closeDialog").onclick = byId("cancelDialog").onclick = function () { byId("cameraDialog").close(); };
byId("closeToken").onclick = function () { byId("tokenDialog").close(); };
byId("closeSnapshot").onclick = function () { byId("snapshotDialog").close(); };
byId("closeOnvif").onclick = function () { byId("onvifDialog").close(); };
byId("closePlayer").onclick = function () { destroyPlayback(); state.playerCamera = null; byId("playerDialog").close(); };
byId("closeSystem").onclick=function(){byId("systemDialog").close();};
byId("closeAdmin").onclick=function(){byId("adminDialog").close();};

document.querySelectorAll(".mosaic-size").forEach(function(button){button.onclick=function(){renderMosaic(Number(button.dataset.size||4));};});

byId("loginForm").onsubmit=async function(e){
  e.preventDefault();
  const form=new FormData(e.target);
  try{
    const result=await fetch("/api/v1/auth/login",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({username:String(form.get("username")||""),password:String(form.get("password")||"")})});
    const data=await result.json().catch(function(){return{};});
    if(!result.ok)throw new Error(data.error||"Falha de login");
    await authenticateToken(data.token);
    byId("tokenDialog").close(); e.target.reset(); notice("Autenticado como "+state.principal.username+".",false);
  }catch(err){notice(err.message,true);}
};

byId("tokenForm").onsubmit = async function (e) {
  e.preventDefault();
  try {
    await authenticateToken(byId("tokenInput").value.trim());
    byId("tokenDialog").close(); e.target.reset(); notice("Token bootstrap autenticado.", false);
  } catch (err) { notice(err.message, true); }
};

byId("scanOnvif").onclick=async function(){
  byId("onvifScanStatus").textContent="Procurando...";byId("onvifDevices").innerHTML="";
  try{const result=await api("/api/v1/onvif/discover?timeout_ms=3000");renderONVIFDevices(result.items||[]);byId("onvifScanStatus").textContent=result.count+" dispositivo(s)";}
  catch(err){byId("onvifScanStatus").textContent=err.message;}
};
byId("onvifDevices").onclick=function(e){const b=e.target.closest("[data-xaddr]");if(!b)return;byId("onvifEndpoint").value=b.dataset.xaddr||"";byId("onvifDeviceInfo").textContent="Endpoint selecionado. Informe as credenciais e leia os perfis.";};
byId("inspectOnvif").onclick=inspectONVIF;
byId("ptzPanel").onclick=function(e){const b=e.target.closest("[data-ptz]");if(b)ptzAction(b.dataset.ptz);};

byId("onvifForm").onsubmit=async function(e){
  e.preventDefault();const form=new FormData(e.target);const select=byId("onvifProfile");const option=select.options[select.selectedIndex];
  if(!option||!option.value){byId("onvifDeviceInfo").textContent="Selecione um perfil ONVIF.";return;}
  const payload={endpoint:String(form.get("endpoint")||"").trim(),username:String(form.get("username")||""),password:String(form.get("password")||""),profile_token:option.value,media_version:Number(option.dataset.version||1),name:String(form.get("name")||"").trim(),city:String(form.get("city")||"").trim(),site:String(form.get("site")||"").trim(),description:String(form.get("description")||"").trim(),enabled:form.get("enabled")==="on"};
  try{await api("/api/v1/cameras/from-onvif",{method:"POST",body:JSON.stringify(payload)});e.target.reset();state.onvifProfiles=[];byId("onvifProfile").innerHTML="<option>Leia os perfis primeiro</option>";byId("onvifProfile").disabled=true;byId("addOnvifCamera").disabled=true;byId("onvifDevices").innerHTML="";byId("onvifDialog").close();notice("Câmera ONVIF adicionada.",false);setTimeout(loadCameras,500);}
  catch(err){byId("onvifDeviceInfo").textContent=err.message;}
};

byId("cameraForm").onsubmit=async function(e){
  e.preventDefault();const form=new FormData(e.target);const snapshot=String(form.get("snapshot_url")||"").trim();
  const payload={name:form.get("name"),city:form.get("city"),site:form.get("site"),rtsp_url:form.get("rtsp_url"),snapshot_url:snapshot||null,description:form.get("description"),enabled:form.get("enabled")==="on"};
  try{await api("/api/v1/cameras",{method:"POST",body:JSON.stringify(payload)});e.target.reset();byId("cameraDialog").close();notice("Câmera cadastrada.",false);setTimeout(loadCameras,500);}
  catch(err){notice(err.message,true);}
};

byId("loadTimeline").onclick=async function(){try{await loadTimelineVisual();await startPlaybackRange();}catch(err){byId("playerStatus").textContent=err.message;}};
byId("timelineBar").onclick=function(e){const b=e.target.closest(".timeline-segment");if(!b)return;byId("timelineFrom").value=inputDate(new Date(b.dataset.start));byId("timelineTo").value=inputDate(new Date(b.dataset.end));startPlaybackRange().catch(function(err){byId("playerStatus").textContent=err.message;});};
byId("exportEvidence").onclick=async function(){
  if(!state.playerCamera||!can("evidence"))return;
  try{const range=timelineRange();const job=await api("/api/v1/cameras/"+state.playerCamera+"/exports",{method:"POST",body:JSON.stringify(range)});notice("Exportação "+job.id+" adicionada à fila.",false);}
  catch(err){notice(err.message,true);}
};

byId("cameraRows").onclick=async function(e){
  const id=e.target.dataset.id;if(!id)return;
  if(e.target.classList.contains("live"))showLive(id);
  if(e.target.classList.contains("play"))showPlayback(id);
  if(e.target.classList.contains("sync-onvif")){notice("Sincronizando ONVIF...",false);try{await api("/api/v1/cameras/"+id+"/onvif/sync",{method:"POST",body:"{}"});notice("ONVIF sincronizado.",false);setTimeout(loadCameras,500);}catch(err){notice(err.message,true);}}
  if(e.target.classList.contains("test")){notice("Testando RTSP...",false);try{const result=await api("/api/v1/cameras/"+id+"/test",{method:"POST"});notice(result.reachable?("RTSP válido em "+result.latency_ms+" ms."):("Falha: "+result.error),!result.reachable);}catch(err){notice(err.message,true);}}
  if(e.target.classList.contains("snap"))showSnapshot(id);
  if(e.target.classList.contains("del")&&confirm("Excluir esta câmera? As gravações existentes não serão apagadas automaticamente.")){try{await api("/api/v1/cameras/"+id,{method:"DELETE"});notice("Câmera excluída.",false);setTimeout(loadCameras,500);}catch(err){notice(err.message,true);}}
};

byId("userForm").onsubmit=async function(e){
  e.preventDefault();const form=new FormData(e.target);
  try{await api("/api/v1/users",{method:"POST",body:JSON.stringify({username:form.get("username"),display_name:form.get("display_name"),password:form.get("password"),role:form.get("role")})});e.target.reset();await loadUsers();}
  catch(err){notice(err.message,true);}
};
byId("usersList").onclick=async function(e){
  const row=e.target.closest("[data-user]");if(!row)return;const id=row.dataset.user;
  try{
    if(e.target.classList.contains("user-save")){await api("/api/v1/users/"+id,{method:"PUT",body:JSON.stringify({display_name:row.querySelector(".muted").textContent,role:row.querySelector(".user-role").value,enabled:row.querySelector(".user-enabled").checked})});}
    if(e.target.classList.contains("user-password")){const pw=prompt("Nova senha (mínimo 10 caracteres):");if(pw)await api("/api/v1/users/"+id+"/password",{method:"PUT",body:JSON.stringify({password:pw})});}
    if(e.target.classList.contains("user-delete")&&confirm("Excluir este usuário?"))await api("/api/v1/users/"+id,{method:"DELETE"});
    await loadUsers();
  }catch(err){notice(err.message,true);}
};
byId("refreshUsers").onclick=function(){loadUsers().catch(function(err){notice(err.message,true);});};
byId("refreshExports").onclick=function(){loadExports().catch(function(err){notice(err.message,true);});};
byId("refreshAudit").onclick=function(){loadAudit().catch(function(err){notice(err.message,true);});};
byId("verifyAudit").onclick=async function(){try{const r=await api("/api/v1/audit/verify");notice(r.valid?"Cadeia de auditoria íntegra.":"Auditoria inválida.",!r.valid);}catch(err){notice(err.message,true);}};
byId("exportsList").onclick=function(e){const b=e.target.closest("[data-export]");if(b)downloadExport(b.dataset.export).catch(function(err){notice(err.message,true);});};

byId("plateSearchForm").onsubmit=function(e){e.preventDefault();searchPlates().catch(function(err){notice(err.message,true);});};
byId("plateEvents").onclick=function(e){const b=e.target.closest("[data-event]");if(b)showPlateEvidence(b.dataset.event);};
byId("hotlistForm").onsubmit=async function(e){
  e.preventDefault();const form=new FormData(e.target);
  try{await api("/api/v1/hotlist",{method:"POST",body:JSON.stringify({plate:form.get("plate"),label:form.get("label"),enabled:true})});e.target.reset();await loadHotlist();notice("Placa adicionada à hotlist.",false);}
  catch(err){notice(err.message,true);}
};
byId("refreshHotlist").onclick=function(){loadHotlist().catch(function(err){notice(err.message,true);});};
byId("hotlistList").onclick=async function(e){
  const row=e.target.closest("[data-hotlist]");if(!row)return;const id=row.dataset.hotlist;
  try{
    if(e.target.classList.contains("hotlist-save")){
      const plate=row.querySelector(".plate-number").textContent;
      const label=row.querySelector(".muted").textContent;
      await api("/api/v1/hotlist/"+encodeURIComponent(id),{method:"PUT",body:JSON.stringify({plate:plate,label:label,enabled:row.querySelector(".hotlist-enabled").checked})});
    }
    if(e.target.classList.contains("hotlist-delete")&&confirm("Remover esta placa da hotlist?")){
      await api("/api/v1/hotlist/"+encodeURIComponent(id),{method:"DELETE"});
    }
    await loadHotlist();
  }catch(err){notice(err.message,true);}
};

health();
updateIdentityUI();
if(state.token){
  authenticateToken(state.token).catch(function(){byId("tokenDialog").showModal();});
}
setInterval(function(){if(state.token)loadCameras();},10000);
