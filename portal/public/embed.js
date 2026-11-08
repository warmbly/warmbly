/* Warmbly portal embed. Renders an auto-resizing iframe for a client connect
 * page or a shared report from data-warmbly-connect="TOKEN" or
 * data-warmbly-report="TOKEN", on a div or on this script tag itself.
 * The page always runs on the portal's own origin, so the host page's CSS
 * and scripts cannot break it and no visitor data flows through the host page. */
(function () {
  "use strict";
  if (window.__warmblyPortalLoaded) return;
  window.__warmblyPortalLoaded = true;

  var script = document.currentScript;
  var base = "";
  if (script && script.src) {
    var a = document.createElement("a");
    a.href = script.src;
    base = a.protocol + "//" + a.host;
  }

  // Bearer-token pages: the token is the whole authorisation, so a report framed on a public site is public.
  var pages = {
    connect: { attr: "data-warmbly-connect", path: "/connect/", title: "Connect your mailbox" },
    report: { attr: "data-warmbly-report", path: "/r/", title: "Report" }
  };

  function makeFrame(token, kind) {
    var page = pages[kind];
    var f = document.createElement("iframe");
    f.src = base + page.path + encodeURIComponent(token) + "?embed=1";
    f.setAttribute("data-warmbly-frame", kind);
    f.setAttribute("title", page.title);
    f.style.width = "100%";
    f.style.border = "0";
    f.style.display = "block";
    f.style.minHeight = "320px";
    f.style.background = "transparent";
    f.setAttribute("allowtransparency", "true");
    f.loading = "lazy";
    return f;
  }

  function mount() {
    for (var kind in pages) {
      if (!pages.hasOwnProperty(kind)) continue;
      var found = document.querySelectorAll("[" + pages[kind].attr + "]");
      for (var j = 0; j < found.length; j++) {
        var node = found[j];
        if (node.getAttribute("data-warmbly-mounted")) continue;
        var token = node.getAttribute(pages[kind].attr);
        if (!token) continue;
        node.setAttribute("data-warmbly-mounted", "1");
        var frame = makeFrame(token, kind);
        // A script tag renders nothing itself, so its frame goes right after it.
        if (node.tagName === "SCRIPT") {
          node.parentNode && node.parentNode.insertBefore(frame, node.nextSibling);
        } else {
          node.appendChild(frame);
        }
      }
    }
  }

  window.addEventListener("message", function (e) {
    if (base && e.origin !== base) return;
    var d = e.data;
    if (!d || typeof d !== "object") return;
    // The connect page names its own messages (portal/src/connectEmbed.ts); a report posts warmbly:resize with page.
    if (d.type === "warmbly:connect:resize") d = { type: "warmbly:resize", page: "connect", height: d.height };
    if (d.type === "warmbly:connect:connected") {
      try {
        document.dispatchEvent(new CustomEvent("warmbly:connected", { detail: { connected: d.connected | 0 } }));
      } catch (err) {}
      return;
    }
    if (d.type === "warmbly:resize" && d.page && pages.hasOwnProperty(d.page)) {
      var frames = document.querySelectorAll('iframe[data-warmbly-frame="' + d.page + '"]');
      for (var i = 0; i < frames.length; i++) {
        if (e.source && frames[i].contentWindow !== e.source) continue;
        frames[i].style.height = Math.max(120, d.height | 0) + "px";
      }
    }
  });

  window.WarmblyPortal = { scan: mount };

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", mount);
  } else {
    mount();
  }
})();
