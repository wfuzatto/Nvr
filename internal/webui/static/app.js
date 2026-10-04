const state = {
  token: sessionStorage.getItem("nvr_admin_token") || "",
  cameras: {},
  hls: null,
  playerCamera: null,
  onvifProfiles: []
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
  if (!response.ok) throw new Error(data.error || ("HTTP " + response.status));
  return data;
}

async function health() {
  try {
    await api("/api/v1/health");
    byId("health").textContent = "Core online";
    byId("health").classList.add("ok");
  } catch (_) {
    byId("health").textContent = "Core offline";
  }
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
    const results = await Promise.all([api("/api/v1/cameras"), api("/api/v1/media/status")]);
    const data = results[0];
    const media = results[1];
    const statuses = {};
    (media.items || []).forEach(function (s) { statuses[s.camera_id] = s; });

    state.cameras = {};
    data.items.forEach(function (camera) { state.cameras[camera.id] = camera; });
    byId("cameraCount").textContent = data.count;
    byId("enabledCount").textContent = data.items.filter(function (x) { return x.enabled; }).length;
    byId("recordingCount").textContent = Object.values(statuses).filter(function (x) { return x.state === "recording"; }).length;

    if (!data.items.length) {
      byId("cameraRows").innerHTML = '<tr><td colspan="5" class="empty">Nenhuma câmera cadastrada.</td></tr>';
      return;
    }

    byId("cameraRows").innerHTML = data.items.map(function (c) {
      const snapButton = c.snapshot_url
        ? '<button class="secondary snap" data-id="' + c.id + '">Snapshot</button> '
        : "";
      const syncButton = c.onvif_url
        ? '<button class="secondary sync-onvif" data-id="' + c.id + '">Sync ONVIF</button> '
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
        "<button class=\"secondary test\" data-id=\"" + c.id + "\">Testar</button> " +
        snapButton + syncButton +
        "<button class=\"danger del\" data-id=\"" + c.id + "\">Excluir</button></td></tr>";
    }).join("");
  } catch (e) {
    notice(e.message, true);
    if (e.message.toLowerCase().indexOf("token") >= 0) byId("tokenDialog").showModal();
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

function destroyPlayback() {
  if (state.hls) {
    state.hls.destroy();
    state.hls = null;
  }
  const video = byId("playerVideo");
  video.pause();
  video.removeAttribute("src");
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
      if (data && data.fatal) {
        byId("playerStatus").textContent = "Falha HLS: " + (data.details || data.type || "erro");
      }
    });
    return true;
  }

  byId("playerStatus").textContent = "Este navegador não possui HLS/MSE compatível.";
  return false;
}

async function showLive(id) {
  const camera = state.cameras[id];
  if (!camera) return;
  state.playerCamera = id;
  byId("playerTitle").textContent = camera.name + " — Ao vivo";
  byId("playerStatus").textContent = "Conectando ao fluxo gravado...";
  byId("ptzPanel").hidden = !camera.onvif_ptz;
  byId("playerDialog").showModal();
  destroyPlayback();

  try {
    const session = await api("/api/v1/cameras/" + id + "/live/session", {
      method:"POST",
      body:JSON.stringify({ttl_seconds:28800})
    });
    attachHLS(session.playlist_url, "Ao vivo HLS local", true);
  } catch (err) {
    byId("playerStatus").textContent = err.message;
  }
}

async function showPlayback(id) {
  const camera = state.cameras[id];
  if (!camera) return;
  state.playerCamera = id;
  byId("playerTitle").textContent = camera.name;
  byId("playerStatus").textContent = "Preparando gravações...";
  byId("ptzPanel").hidden = !camera.onvif_ptz;
  byId("playerDialog").showModal();
  destroyPlayback();

  try {
    const timeline = await api("/api/v1/cameras/" + id + "/timeline?limit=20");
    const playable = (timeline.items || []).some(function (item) { return !!item.frames_path; });
    if (!playable) {
      byId("playerStatus").textContent = "Ainda não há segmentos novos com índice de playback.";
      return;
    }

    const session = await api("/api/v1/cameras/" + id + "/playback/session", {
      method: "POST",
      body: JSON.stringify({limit: 120, ttl_seconds: 900})
    });
    attachHLS(session.playlist_url, "Playback HLS local", false);
  } catch (err) {
    byId("playerStatus").textContent = err.message;
  }
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
      '<strong>' + escapeHTML(name || "Câmera ONVIF") + '</strong>' +
      '<span>' + escapeHTML(xaddr) + '</span>' +
      '<small>' + escapeHTML(device.from || "") + '</small></button>';
  }).join("");
}

async function inspectONVIF() {
  const form = new FormData(byId("onvifForm"));
  const endpoint = String(form.get("endpoint") || "").trim();
  if (!endpoint) {
    byId("onvifDeviceInfo").textContent = "Informe ou descubra um endpoint.";
    return;
  }
  byId("onvifDeviceInfo").textContent = "Consultando...";
  byId("addOnvifCamera").disabled = true;
  try {
    const result = await api("/api/v1/onvif/inspect", {
      method: "POST",
      body: JSON.stringify({
        endpoint: endpoint,
        username: String(form.get("username") || ""),
        password: String(form.get("password") || "")
      })
    });
    state.onvifProfiles = result.profiles || [];
    const select = byId("onvifProfile");
    select.innerHTML = state.onvifProfiles.map(function (profile, index) {
      const title = (profile.name || ("Perfil " + (index + 1))) +
        " · " + (profile.encoding || "?") +
        (profile.width ? (" " + profile.width + "×" + profile.height) : "") +
        (profile.frame_rate_limit ? (" @" + profile.frame_rate_limit + "fps") : "") +
        (profile.ptz ? " · PTZ" : "");
      return '<option value="' + escapeHTML(profile.token) + '" data-version="' + profile.media_version + '">' + escapeHTML(title) + '</option>';
    }).join("");
    select.disabled = state.onvifProfiles.length === 0;
    byId("addOnvifCamera").disabled = state.onvifProfiles.length === 0;

    const device = result.device || {};
    const summary = [device.manufacturer, device.model, device.firmware_version].filter(Boolean).join(" · ");
    byId("onvifDeviceInfo").textContent = summary || (state.onvifProfiles.length + " perfil(is)");
    const nameInput = byId("onvifForm").querySelector('input[name="name"]');
    if (!nameInput.value && (device.model || device.manufacturer)) {
      nameInput.value = [device.manufacturer, device.model].filter(Boolean).join(" ");
    }
  } catch (err) {
    byId("onvifDeviceInfo").textContent = err.message;
  }
}

async function ptzAction(action) {
  const id = state.playerCamera;
  if (!id) return;
  if (action === "stop") {
    try { await api("/api/v1/cameras/" + id + "/ptz/stop", {method:"POST", body:"{}"}); } catch (_) {}
    return;
  }
  const moves = {
    up: {pan:0, tilt:0.6, zoom:0},
    down: {pan:0, tilt:-0.6, zoom:0},
    left: {pan:-0.6, tilt:0, zoom:0},
    right: {pan:0.6, tilt:0, zoom:0},
    zoomin: {pan:0, tilt:0, zoom:0.6},
    zoomout: {pan:0, tilt:0, zoom:-0.6}
  };
  const move = moves[action];
  if (!move) return;
  try {
    await api("/api/v1/cameras/" + id + "/ptz/move", {
      method:"POST",
      body:JSON.stringify(Object.assign({timeout_ms:650}, move))
    });
  } catch (err) {
    byId("playerStatus").textContent = "PTZ: " + err.message;
  }
}

byId("authBtn").onclick = function () { byId("tokenDialog").showModal(); };
byId("addBtn").onclick = function () { (state.token ? byId("cameraDialog") : byId("tokenDialog")).showModal(); };
byId("discoverBtn").onclick = function () { (state.token ? byId("onvifDialog") : byId("tokenDialog")).showModal(); };
byId("closeDialog").onclick = byId("cancelDialog").onclick = function () { byId("cameraDialog").close(); };
byId("closeToken").onclick = function () { byId("tokenDialog").close(); };
byId("closeSnapshot").onclick = function () { byId("snapshotDialog").close(); };
byId("closeOnvif").onclick = function () { byId("onvifDialog").close(); };
byId("closePlayer").onclick = function () { destroyPlayback(); state.playerCamera = null; byId("playerDialog").close(); };

byId("scanOnvif").onclick = async function () {
  byId("onvifScanStatus").textContent = "Procurando...";
  byId("onvifDevices").innerHTML = "";
  try {
    const result = await api("/api/v1/onvif/discover?timeout_ms=3000");
    renderONVIFDevices(result.items || []);
    byId("onvifScanStatus").textContent = result.count + " dispositivo(s)";
  } catch (err) {
    byId("onvifScanStatus").textContent = err.message;
  }
};

byId("onvifDevices").onclick = function (e) {
  const button = e.target.closest("[data-xaddr]");
  if (!button) return;
  byId("onvifEndpoint").value = button.dataset.xaddr || "";
  byId("onvifDeviceInfo").textContent = "Endpoint selecionado. Informe as credenciais e leia os perfis.";
};

byId("inspectOnvif").onclick = inspectONVIF;

byId("ptzPanel").onclick = function (e) {
  const button = e.target.closest("[data-ptz]");
  if (button) ptzAction(button.dataset.ptz);
};

byId("tokenForm").onsubmit = async function (e) {
  e.preventDefault();
  state.token = byId("tokenInput").value.trim();
  sessionStorage.setItem("nvr_admin_token", state.token);
  try {
    await api("/api/v1/cameras");
    byId("tokenDialog").close();
    notice("Autenticado.", false);
    loadCameras();
  } catch (err) { notice(err.message, true); }
};

byId("onvifForm").onsubmit = async function (e) {
  e.preventDefault();
  const form = new FormData(e.target);
  const select = byId("onvifProfile");
  const option = select.options[select.selectedIndex];
  if (!option || !option.value) {
    byId("onvifDeviceInfo").textContent = "Selecione um perfil ONVIF.";
    return;
  }
  const payload = {
    endpoint: String(form.get("endpoint") || "").trim(),
    username: String(form.get("username") || ""),
    password: String(form.get("password") || ""),
    profile_token: option.value,
    media_version: Number(option.dataset.version || 1),
    name: String(form.get("name") || "").trim(),
    city: String(form.get("city") || "").trim(),
    site: String(form.get("site") || "").trim(),
    description: String(form.get("description") || "").trim(),
    enabled: form.get("enabled") === "on"
  };
  try {
    const result = await api("/api/v1/cameras/from-onvif", {
      method:"POST",
      body:JSON.stringify(payload)
    });
    e.target.reset();
    state.onvifProfiles = [];
    byId("onvifProfile").innerHTML = "<option>Leia os perfis primeiro</option>";
    byId("onvifProfile").disabled = true;
    byId("addOnvifCamera").disabled = true;
    byId("onvifDevices").innerHTML = "";
    byId("onvifDeviceInfo").textContent = "";
    byId("onvifDialog").close();
    notice("Câmera ONVIF adicionada com perfil " + (result.profile.name || result.profile.token) + ".", false);
    setTimeout(loadCameras, 500);
  } catch (err) {
    byId("onvifDeviceInfo").textContent = err.message;
  }
};

byId("cameraForm").onsubmit = async function (e) {
  e.preventDefault();
  const form = new FormData(e.target);
  const snapshot = String(form.get("snapshot_url") || "").trim();
  const payload = {
    name: form.get("name"),
    city: form.get("city"),
    site: form.get("site"),
    rtsp_url: form.get("rtsp_url"),
    snapshot_url: snapshot || null,
    description: form.get("description"),
    enabled: form.get("enabled") === "on"
  };
  try {
    await api("/api/v1/cameras", {method:"POST", body:JSON.stringify(payload)});
    e.target.reset();
    byId("cameraDialog").close();
    notice("Câmera cadastrada. O Media Engine iniciará a gravação automaticamente.", false);
    setTimeout(loadCameras, 500);
  } catch (err) { notice(err.message, true); }
};

byId("cameraRows").onclick = async function (e) {
  const id = e.target.dataset.id;
  if (!id) return;

  if (e.target.classList.contains("live")) {
    showLive(id);
  }

  if (e.target.classList.contains("play")) {
    showPlayback(id);
  }

  if (e.target.classList.contains("sync-onvif")) {
    notice("Sincronizando perfil ONVIF...", false);
    try {
      await api("/api/v1/cameras/" + id + "/onvif/sync", {method:"POST", body:"{}"});
      notice("ONVIF sincronizado; RTSP e snapshot atualizados.", false);
      setTimeout(loadCameras, 500);
    } catch (err) { notice(err.message, true); }
  }

  if (e.target.classList.contains("test")) {
    notice("Testando RTSP...", false);
    try {
      const result = await api("/api/v1/cameras/" + id + "/test", {method:"POST"});
      const tracks = result.rtsp ? (" vídeo=" + result.rtsp.video_tracks + " áudio=" + result.rtsp.audio_tracks) : "";
      notice(result.reachable ? ("RTSP válido em " + result.latency_ms + " ms." + tracks) : ("Falha: " + result.error), !result.reachable);
    } catch (err) { notice(err.message, true); }
  }

  if (e.target.classList.contains("snap")) {
    showSnapshot(id);
  }

  if (e.target.classList.contains("timeline")) {
    notice("Carregando timeline...", false);
    try {
      const result = await api("/api/v1/cameras/" + id + "/timeline?limit=100");
      if (!result.count) {
        notice("Ainda não há segmentos finalizados para esta câmera.", false);
      } else {
        const first = result.items[0];
        const last = result.items[result.items.length - 1];
        notice(result.count + " segmento(s). De " + new Date(first.start).toLocaleString() + " até " + new Date(last.end).toLocaleString() + ".", false);
      }
    } catch (err) { notice(err.message, true); }
  }

  if (e.target.classList.contains("del") && confirm("Excluir esta câmera? As gravações existentes não serão apagadas automaticamente.")) {
    try {
      await api("/api/v1/cameras/" + id, {method:"DELETE"});
      notice("Câmera excluída. O worker de gravação será encerrado.", false);
      setTimeout(loadCameras, 500);
    } catch (err) { notice(err.message, true); }
  }
};

health();
if (state.token) loadCameras();
setInterval(function () { if (state.token) loadCameras(); }, 10000);
