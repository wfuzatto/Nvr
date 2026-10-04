const state = { token: sessionStorage.getItem("nvr_admin_token") || "" };
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

async function loadCameras() {
  if (!state.token) return;
  try {
    const data = await api("/api/v1/cameras");
    byId("cameraCount").textContent = data.count;
    byId("enabledCount").textContent = data.items.filter(function (x) { return x.enabled; }).length;

    if (!data.items.length) {
      byId("cameraRows").innerHTML = '<tr><td colspan="5" class="empty">Nenhuma câmera cadastrada.</td></tr>';
      return;
    }

    byId("cameraRows").innerHTML = data.items.map(function (c) {
      return "<tr>" +
        "<td><strong>" + escapeHTML(c.name) + "</strong><div class=\"muted small\">" + escapeHTML(c.description || "") + "</div></td>" +
        "<td>" + escapeHTML(c.city || "—") + "<div class=\"muted small\">" + escapeHTML(c.site || "") + "</div></td>" +
        "<td><code>" + escapeHTML(c.rtsp_url) + "</code></td>" +
        "<td><span class=\"pill " + (c.enabled ? "ok" : "") + "\">" + (c.enabled ? "habilitada" : "desabilitada") + "</span></td>" +
        "<td class=\"row-actions\"><button class=\"secondary test\" data-id=\"" + c.id + "\">Testar</button> " +
        "<button class=\"danger del\" data-id=\"" + c.id + "\">Excluir</button></td></tr>";
    }).join("");
  } catch (e) {
    notice(e.message, true);
    if (e.message.toLowerCase().indexOf("token") >= 0) byId("tokenDialog").showModal();
  }
}

byId("authBtn").onclick = function () { byId("tokenDialog").showModal(); };
byId("addBtn").onclick = function () { (state.token ? byId("cameraDialog") : byId("tokenDialog")).showModal(); };
byId("closeDialog").onclick = byId("cancelDialog").onclick = function () { byId("cameraDialog").close(); };
byId("closeToken").onclick = function () { byId("tokenDialog").close(); };

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
  const payload = {
    name: form.get("name"),
    city: form.get("city"),
    site: form.get("site"),
    rtsp_url: form.get("rtsp_url"),
    description: form.get("description"),
    enabled: form.get("enabled") === "on"
  };
  try {
    await api("/api/v1/cameras", {method:"POST", body:JSON.stringify(payload)});
    e.target.reset();
    byId("cameraDialog").close();
    notice("Câmera cadastrada.", false);
    loadCameras();
  } catch (err) { notice(err.message, true); }
};

byId("cameraRows").onclick = async function (e) {
  const id = e.target.dataset.id;
  if (!id) return;

  if (e.target.classList.contains("test")) {
    notice("Testando conectividade...", false);
    try {
      const result = await api("/api/v1/cameras/" + id + "/test", {method:"POST"});
      notice(result.reachable ? ("Câmera alcançável em " + result.latency_ms + " ms.") : ("Falha: " + result.error), !result.reachable);
    } catch (err) { notice(err.message, true); }
  }

  if (e.target.classList.contains("del") && confirm("Excluir esta câmera?")) {
    try {
      await api("/api/v1/cameras/" + id, {method:"DELETE"});
      notice("Câmera excluída.", false);
      loadCameras();
    } catch (err) { notice(err.message, true); }
  }
};

health();
if (state.token) loadCameras();
