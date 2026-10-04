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
        "<td class=\"row-actions\"><button class=\"play\" data-id=\"" + c.id + "\">Playback</button> " +
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

byId("authBtn").onclick = function () { byId("tokenDialog").showModal(); };
byId("addBtn").onclick = function () { (state.token ? byId("cameraDialog") : byId("tokenDialog")).showModal(); };
byId("closeDialog").onclick = byId("cancelDialog").onclick = function () { byId("cameraDialog").close(); };
byId("closeToken").onclick = function () { byId("tokenDialog").close(); };
byId("closeSnapshot").onclick = function () { byId("snapshotDialog").close(); };

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
