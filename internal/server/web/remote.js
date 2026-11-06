"use strict";
(() => {
  const $ = (s) => document.querySelector(s);
  let base = "";       // http://host:port
  let code = "";
  let down = false;    // left button held (for drag)
  let moveQueue = [];
  let flushTimer = null;

  // Prefill host from the current origin (handy when the viewer page is opened
  // from the host itself for a self-test).
  $("#host").value = location.host;

  $("#go").addEventListener("click", connect);
  $("#code").addEventListener("keydown", (e) => { if (e.key === "Enter") connect(); });

  // Auto-connect when opened from a scanned QR (?host=ip:port&code=XXXX).
  try {
    const q = new URLSearchParams(location.search);
    const qHost = q.get("host"), qCode = q.get("code");
    if (qHost) {
      $("#host").value = qHost;
      if (qCode) $("#code").value = qCode;
      setTimeout(connect, 300);
    }
  } catch (e) {}

  async function connect() {
    const h = $("#host").value.trim().replace(/^https?:\/\//, "").replace(/\/$/, "");
    code = $("#code").value.trim();
    if (!h) { showErr("Enter the host address"); return; }
    base = "http://" + h;
    showErr("");
    try {
      const r = await fetch(base + "/api/remote/status?code=" + encodeURIComponent(code), { mode: "cors" });
      if (!r.ok) { showErr("Host reachable but pairing failed — check the code."); return; }
      const st = await r.json();
      if (!st.hosting) { showErr("That device is not hosting remote desktop yet."); return; }
      startView(st);
    } catch (e) {
      showErr("Could not reach the host. Check Wi‑Fi / address.");
    }
  }

  function startView(st) {
    $("#connectView").style.display = "none";
    $("#viewView").style.display = "flex";
    $("#disconnectBtn").style.display = "";
    $("#kbBtn").style.display = "";
    $("#dims").textContent = st.width + "×" + st.height + " · " + st.capture;

    const img = $("#screen");
    img.src = base + "/api/remote/stream?code=" + encodeURIComponent(code) + "&t=" + Date.now();

    bindControl(img);
  }

  $("#disconnectBtn").addEventListener("click", () => location.reload());
  $("#kbBtn").addEventListener("click", () => {
    const kb = $("#kbrow");
    kb.style.display = kb.style.display === "none" ? "flex" : "none";
    if (kb.style.display === "flex") $("#typebox").focus();
  });

  function norm(img, clientX, clientY) {
    const r = img.getBoundingClientRect();
    let x = (clientX - r.left) / r.width;
    let y = (clientY - r.top) / r.height;
    return [Math.max(0, Math.min(1, x)), Math.max(0, Math.min(1, y))];
  }

  function queue(ev) {
    moveQueue.push(ev);
    if (!flushTimer) flushTimer = setTimeout(flush, 30); // batch ~33/s
  }
  function send(ev) { queue(ev); }
  async function flush() {
    flushTimer = null;
    if (!moveQueue.length) return;
    const batch = moveQueue; moveQueue = [];
    try {
      await fetch(base + "/api/remote/input?code=" + encodeURIComponent(code), {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify(batch), keepalive: true
      });
    } catch (e) {}
  }

  function bindControl(img) {
    // Mouse
    img.addEventListener("mousemove", (e) => {
      const [x, y] = norm(img, e.clientX, e.clientY);
      send({ type: "move", x, y });
    });
    img.addEventListener("mousedown", (e) => {
      e.preventDefault();
      const [x, y] = norm(img, e.clientX, e.clientY);
      send({ type: "down", x, y, button: e.button });
    });
    img.addEventListener("mouseup", (e) => {
      e.preventDefault();
      send({ type: "up", button: e.button });
    });
    img.addEventListener("contextmenu", (e) => e.preventDefault());
    img.addEventListener("wheel", (e) => {
      e.preventDefault();
      send({ type: "scroll", dx: Math.sign(e.deltaX) * 3, dy: Math.sign(e.deltaY) * 3 });
    }, { passive: false });

    // Touch (tap = click, drag = move+drag)
    let touchDown = false;
    img.addEventListener("touchstart", (e) => {
      if (!e.touches.length) return;
      const t = e.touches[0];
      const [x, y] = norm(img, t.clientX, t.clientY);
      touchDown = true;
      send({ type: "down", x, y, button: 0 });
      e.preventDefault();
    }, { passive: false });
    img.addEventListener("touchmove", (e) => {
      if (!e.touches.length) return;
      const t = e.touches[0];
      const [x, y] = norm(img, t.clientX, t.clientY);
      send({ type: "move", x, y });
      e.preventDefault();
    }, { passive: false });
    img.addEventListener("touchend", () => {
      if (touchDown) send({ type: "up", button: 0 });
      touchDown = false;
    });

    // Physical keyboard
    window.addEventListener("keydown", (e) => {
      if (document.activeElement === $("#typebox")) return;
      send({ type: "key", code: e.code, down: true });
      if (e.code !== "F5" && e.code !== "F12") e.preventDefault();
    });
    window.addEventListener("keyup", (e) => {
      if (document.activeElement === $("#typebox")) return;
      send({ type: "key", code: e.code, down: false });
    });

    // On-screen type box
    const tb = $("#typebox");
    tb.addEventListener("keydown", (e) => {
      if (e.key === "Enter") {
        if (tb.value) send({ type: "text", text: tb.value });
        send({ type: "key", code: "Enter", down: true });
        send({ type: "key", code: "Enter", down: false });
        tb.value = "";
        e.preventDefault();
      }
    });
    document.querySelectorAll(".k").forEach((b) => b.addEventListener("click", () => {
      send({ type: "key", code: b.dataset.code, down: true });
      send({ type: "key", code: b.dataset.code, down: false });
    }));
  }

  function showErr(m) { $("#err").textContent = m; }
})();
