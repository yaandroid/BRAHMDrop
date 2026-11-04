"use strict";
(() => {
  const $ = (s) => document.querySelector(s);
  const $$ = (s) => document.querySelectorAll(s);
  let ME = null;                 // /api/info
  let TARGET = null;             // {name, mode:'self'|'peer', id?, url?}
  const KIND_ICON = { windows:"🖥️", mac:"💻", linux:"🐧", android:"📱", web:"🌐" };
  const icon = (k) => KIND_ICON[k] || "🌐";

  // ---- tabs ----
  function activateTab(name) {
    const tab = document.querySelector('.tab[data-tab="' + name + '"]');
    if (!tab) return;
    $$(".tab").forEach(x => x.classList.remove("active"));
    $$(".panel").forEach(x => x.classList.remove("active"));
    tab.classList.add("active");
    const panel = $("#panel-" + name);
    if (panel) panel.classList.add("active");
    try { localStorage.setItem("bd_tab", name); } catch (e) {}
  }
  $$(".tab").forEach(t => t.addEventListener("click", () => activateTab(t.dataset.tab)));
  // Restore the last tab so an occasional reload never jumps back to Send.
  try {
    const saved = localStorage.getItem("bd_tab");
    if (saved) activateTab(saved);
  } catch (e) {}

  // ---- toast ----
  let toastT;
  function toast(msg, kind) {
    const el = $("#toast");
    el.textContent = msg;
    el.className = "toast" + (kind ? " " + kind : "");
    el.hidden = false;
    clearTimeout(toastT);
    toastT = setTimeout(() => (el.hidden = true), 3200);
  }

  // ---- load device info ----
  async function loadInfo() {
    const r = await fetch("/api/info");
    ME = await r.json();
    $("#meName").textContent = ME.device.name;
    $("#myUrl").textContent = ME.url;
    $("#saveDir").textContent = ME.save_dir;
    $("#setName").value = ME.device.name;
    $("#setAuto").checked = !!ME.auto_accept;
    $("#aboutVer").textContent = "v" + ME.version;
    $("#aboutCompany").textContent = ME.company;
    $("#aboutDev").textContent = "Developed by " + ME.developer;
    $("#footCompany").textContent = ME.company;
    $("#qrImg").src = "/api/qr?text=" + encodeURIComponent(ME.url);
  }

  // ---- peers ----
  async function loadPeers() {
    let peers = [];
    try { peers = (await (await fetch("/api/peers")).json()).peers || []; } catch (e) {}
    const list = $("#deviceList");
    list.innerHTML = "";

    // "this device" tile — send straight to the device hosting this page
    if (ME) list.appendChild(deviceTile({
      name: ME.device.name, kind: ME.device.kind, sub: "this device · receives here",
      me: true, target: { name: ME.device.name, mode: "self" }
    }));

    peers.forEach(p => list.appendChild(deviceTile({
      name: p.name, kind: p.kind, sub: p.ip + " · tap to send",
      me: false, target: { name: p.name, mode: "peer", id: p.id }
    })));

    $("#noDevices").hidden = peers.length > 0;
  }

  function deviceTile({ name, kind, sub, me, target }) {
    const el = document.createElement("div");
    el.className = "device" + (me ? " me" : "");
    el.innerHTML = `
      <div class="dev-ic">${icon(kind)}</div>
      <div class="dev-main">
        <div class="dev-name">${esc(name)}${me ? '<span class="badge">YOU</span>' : ''}</div>
        <div class="dev-sub">${esc(sub)}</div>
      </div>
      <div class="dev-go">Send ›</div>`;
    el.addEventListener("click", () => openSend(target));
    el.addEventListener("dragover", (e) => { e.preventDefault(); el.style.borderColor = "var(--blue)"; });
    el.addEventListener("dragleave", () => { el.style.borderColor = ""; });
    el.addEventListener("drop", (e) => {
      e.preventDefault(); el.style.borderColor = "";
      if (e.dataTransfer.files.length) { openSend(target); startUpload(e.dataTransfer.files); }
    });
    return el;
  }

  // ---- send sheet ----
  const sheet = $("#sendSheet");
  function openSend(target) {
    TARGET = target;
    $("#sendTarget").textContent = target.name;
    $("#sendBody").hidden = false;
    $("#sendProgress").hidden = true;
    $("#sendDone").hidden = true;
    sheet.hidden = false;
  }
  function closeSend() { sheet.hidden = true; TARGET = null; }
  $("#sendClose").addEventListener("click", closeSend);
  sheet.addEventListener("click", (e) => { if (e.target === sheet) closeSend(); });
  $("#pickFilesBtn").addEventListener("click", () => $("#filePicker").click());
  $("#sendAnother").addEventListener("click", () => {
    $("#sendBody").hidden = false; $("#sendDone").hidden = true;
  });
  $("#filePicker").addEventListener("change", (e) => {
    if (e.target.files.length) startUpload(e.target.files);
    e.target.value = "";
  });

  // drop anywhere on the send sheet body
  $("#sendBody").addEventListener("dragover", (e) => e.preventDefault());
  $("#sendBody").addEventListener("drop", (e) => {
    e.preventDefault();
    if (e.dataTransfer.files.length) startUpload(e.dataTransfer.files);
  });

  function startUpload(files) {
    if (!TARGET) return;
    const fd = new FormData();
    let total = 0;
    for (const f of files) { fd.append("files", f, f.name); total += f.size; }

    let url, setFrom = false;
    if (TARGET.mode === "self") { url = "/api/upload"; setFrom = true; }
    else { url = "/api/send?peer=" + encodeURIComponent(TARGET.id); }

    $("#sendBody").hidden = true;
    $("#sendProgress").hidden = false;
    $("#progBar").style.width = "0%";
    $("#progText").textContent = `Sending ${files.length} file${files.length>1?"s":""} (${human(total)})…`;

    const xhr = new XMLHttpRequest();
    xhr.open("POST", url);
    if (setFrom && ME) {
      xhr.setRequestHeader("X-BRAHM-From", ME.device.name);
      xhr.setRequestHeader("X-BRAHM-Self", "1"); // sending to this same device — never prompt
    }
    xhr.upload.onprogress = (ev) => {
      if (ev.lengthComputable) {
        const pct = Math.round(ev.loaded / ev.total * 100);
        $("#progBar").style.width = pct + "%";
        $("#progText").textContent = pct + "%  (" + human(ev.loaded) + " / " + human(ev.total) + ")";
      }
    };
    xhr.onload = () => {
      if (xhr.status === 200) {
        $("#sendProgress").hidden = true;
        $("#sendDone").hidden = false;
        $("#doneText").textContent = `Sent ${files.length} file${files.length>1?"s":""} to ${TARGET.name}`;
        toast("Transfer complete", "ok");
        loadHistory();
      } else {
        fail(xhr.responseText || ("error " + xhr.status));
      }
    };
    xhr.onerror = () => fail("Could not reach the device. Check Wi-Fi / firewall.");
    xhr.send(fd);
  }
  function fail(msg) {
    $("#sendProgress").hidden = true;
    $("#sendBody").hidden = false;
    toast(msg.slice(0, 120), "err");
  }

  // ---- history ----
  async function loadHistory() {
    let recs = [];
    try { recs = (await (await fetch("/api/history")).json()).records || []; } catch (e) {}
    const list = $("#historyList");
    list.innerHTML = "";
    $("#noHistory").hidden = recs.length > 0;
    recs.forEach(r => {
      const el = document.createElement("div");
      el.className = "hrow";
      const cls = r.status === "error" ? "h-err" : (r.direction === "in" ? "h-in" : "h-out");
      const ic = r.status === "error" ? "⚠️" : (r.direction === "in" ? "⬇️" : "⬆️");
      const verb = r.direction === "in" ? "from" : "to";
      const sub = r.status === "error"
        ? "Failed: " + (r.error || "")
        : `${verb} ${esc(r.peer)} · ${human(r.bytes)} · ${timeago(r.time)}`;

      const files = r.files || [];
      const filesHtml = files.length
        ? files.map(f => {
            const label = fileIcon(f.name) + " " + esc(f.name);
            if (r.direction === "in" && f.saved) {
              return `<span class="h-file" data-saved="${esc(f.saved)}">${label}</span>`;
            }
            return `<span class="h-file plain">${label}</span>`;
          }).join("")
        : '<span class="h-file plain">(no files)</span>';

      el.innerHTML = `<div class="h-ic ${cls}">${ic}</div>
        <div class="h-main"><div class="h-files">${filesHtml}</div><div class="h-sub">${sub}</div></div>`;
      list.appendChild(el);
      el.querySelectorAll(".h-file[data-saved]").forEach(sp => {
        sp.addEventListener("click", () => openFile(sp.dataset.saved));
      });
    });
  }
  $("#clearHist").addEventListener("click", async () => {
    await fetch("/api/history/clear", { method: "POST" });
    loadHistory();
  });

  // ---- internet relay ----
  async function refreshRelay() {
    try {
      const d = await (await fetch("/api/relay/status")).json();
      if (d.enabled) {
        $("#relayInfo").hidden = false;
        $("#relayCode").textContent = d.code || "———";
        $("#relayDot").style.color = d.connected ? "#16a34a" : "#f59e0b";
        if (ME && ME.relay_addr) $("#relayAddr").value = ME.relay_addr;
      }
    } catch (e) {}
  }
  $("#saveRelay")?.addEventListener("click", async () => {
    const addr = $("#relayAddr").value.trim();
    await fetch("/api/settings", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ relay_addr: addr }) });
    toast(addr ? "Relay set — connecting…" : "Internet off", "ok");
    setTimeout(refreshRelay, 1200);
  });
  $("#connectCode")?.addEventListener("click", async () => {
    const code = $("#connCode").value.trim();
    if (!code) { toast("Enter a code", "err"); return; }
    try {
      const r = await fetch("/api/relay/connect?code=" + encodeURIComponent(code));
      if (!r.ok) { toast(await r.text(), "err"); return; }
      const d = await r.json();
      toast("Connected — opening device", "ok");
      window.open(d.url, "_blank");   // opens the remote device's full UI over the internet
    } catch (e) { toast("Could not connect", "err"); }
  });

  // ---- remote desktop ----
  $("#remoteHost")?.addEventListener("change", async (e) => {
    if (e.target.checked) {
      try {
        const r = await fetch("/api/remote/enable", { method: "POST" });
        const d = await r.json();
        $("#remoteCode").textContent = d.code || "------";
        $("#remoteAddr").textContent = (ME ? ME.url : location.origin);
        $("#remoteBackend").textContent = "Screen: " + (d.capture||"") + " · Input: " + (d.input||"");
        $("#remoteInfo").hidden = false;
        toast("Remote hosting ON", "ok");
      } catch (x) { toast("Could not enable remote", "err"); e.target.checked = false; }
    } else {
      await fetch("/api/remote/disable", { method: "POST" });
      $("#remoteInfo").hidden = true;
      toast("Remote hosting OFF");
    }
  });
  async function refreshRemote() {
    try {
      const d = await (await fetch("/api/remote/status")).json();
      if (d.hosting) {
        $("#remoteHost").checked = true;
        $("#remoteCode").textContent = d.code || "------";
        $("#remoteAddr").textContent = (ME ? ME.url : location.origin);
        $("#remoteBackend").textContent = "Screen: " + (d.capture||"") + " · Input: " + (d.input||"");
        $("#remoteInfo").hidden = false;
      }
    } catch (x) {}
  }

  // ---- settings ----
  $("#saveSettings").addEventListener("click", async () => {
    const body = { device_name: $("#setName").value.trim(), auto_accept: $("#setAuto").checked };
    await fetch("/api/settings", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
    toast("Saved", "ok");
    loadInfo();
  });

  // ---- add by IP ----
  $("#addByIpBtn")?.addEventListener("click", async () => {
    const ip = prompt("Enter the other device's IP (shown on its Receive screen):");
    if (!ip) return;
    const r = await fetch("/api/peers/add", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ ip: ip.trim(), port: 53330 })
    });
    if (r.ok) { toast("Device added", "ok"); loadPeers(); }
    else { toast(await r.text(), "err"); }
  });

  // ---- incoming accept dialog ----
  const askSheet = $("#askSheet");
  let askId = null;
  function showAsk(id, from) {
    askId = id;
    $("#askText").textContent = `${from} wants to send you files. Accept?`;
    askSheet.hidden = false;
  }
  async function respond(accept) {
    askSheet.hidden = true;
    if (!askId) return;
    await fetch("/api/respond", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ id: askId, accept }) });
    askId = null;
  }
  $("#askAccept").addEventListener("click", () => respond(true));
  $("#askDecline").addEventListener("click", () => respond(false));

  // ---- live events (SSE) ----
  function connectSSE() {
    const es = new EventSource("/api/events");
    es.onopen = () => { $("#statusDot").classList.add("on"); $("#statusText").textContent = "online"; };
    es.onerror = () => { $("#statusDot").classList.remove("on"); $("#statusText").textContent = "reconnecting…"; };
    es.addEventListener("peers", loadPeers);
    es.addEventListener("history", loadHistory);
    es.addEventListener("incoming", (e) => {
      try { const d = JSON.parse(e.data); toast(`Received ${d.count} file(s) from ${d.from}`, "ok"); } catch (x) {}
      loadHistory();
    });
    es.addEventListener("ask", (e) => {
      try { const d = JSON.parse(e.data); showAsk(d.id, d.from); } catch (x) {}
    });
  }

  // ---- open a received file ----
  function openFile(saved) {
    if (!saved) return;
    // On Android, use the native bridge so it opens in the gallery / player / viewer.
    if (window.AndroidBridge && window.AndroidBridge.openFile) {
      try { window.AndroidBridge.openFile(saved); return; } catch (e) {}
    }
    // Desktop / browser: open via the local server (images, video, pdf, audio
    // render inline; other types download).
    window.open("/api/file?path=" + encodeURIComponent(saved), "_blank");
  }
  function fileIcon(name) {
    const e = (name.split(".").pop() || "").toLowerCase();
    if (["jpg","jpeg","png","gif","webp","bmp","heic","svg"].includes(e)) return "🖼️";
    if (["mp4","mkv","mov","avi","webm","3gp","m4v"].includes(e)) return "🎬";
    if (["mp3","wav","aac","ogg","m4a","flac"].includes(e)) return "🎵";
    if (["pdf"].includes(e)) return "📄";
    if (["doc","docx","xls","xlsx","ppt","pptx","txt","csv"].includes(e)) return "📝";
    if (["zip","rar","7z","tar","gz"].includes(e)) return "🗜️";
    if (["apk"].includes(e)) return "📦";
    return "📎";
  }

  // ---- helpers ----
  function esc(s) { return String(s).replace(/[&<>"']/g, c => ({ "&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;" }[c])); }
  function human(b) {
    if (b < 1024) return b + " B";
    const u = ["KB","MB","GB","TB"]; let i = -1;
    do { b /= 1024; i++; } while (b >= 1024 && i < u.length - 1);
    return b.toFixed(1) + " " + u[i];
  }
  function timeago(t) {
    const d = (Date.now() - new Date(t).getTime()) / 1000;
    if (d < 60) return "just now";
    if (d < 3600) return Math.floor(d/60) + "m ago";
    if (d < 86400) return Math.floor(d/3600) + "h ago";
    return new Date(t).toLocaleDateString();
  }

  // ---- service worker (PWA) ----
  if ("serviceWorker" in navigator) {
    navigator.serviceWorker.register("/sw.js").catch(() => {});
  }

  // ---- boot ----
  (async function init() {
    await loadInfo();
    await loadPeers();
    await loadHistory();
    await refreshRemote();
    await refreshRelay();
    connectSSE();
    setInterval(loadPeers, 5000); // safety refresh in case SSE drops
  })();
})();
