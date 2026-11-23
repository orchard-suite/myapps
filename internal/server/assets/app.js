// The page's own behaviour: the launcher and the palette, the dock of
// favorite and recent apps, hidden apps, a tile's sheet, live status, the landscape's views, zoom, and panels, and
// the browser telemetry. Everything works without it; it only makes the
// page livelier. The admin grid is grid.js, and needs it.
(function () {
  "use strict";
  var cfg = window.__myapps || {};
  var faro = null;
  var all = function (sel, root) { return Array.prototype.slice.call((root || document).querySelectorAll(sel)); };

  // Browser telemetry, only when the server says the relay is on. Console
  // capture and resource timing stay off; spans cover the page's own API.
  if (cfg.faro && window.GrafanaFaroWebSdk) {
    try {
      var sdk = window.GrafanaFaroWebSdk;
      var instrumentations = sdk.getWebInstrumentations({ captureConsole: false, enablePerformanceInstrumentation: false });
      if (window.GrafanaFaroWebTracing) instrumentations.push(new window.GrafanaFaroWebTracing.TracingInstrumentation());
      faro = sdk.initializeFaro({
        url: "/api/telemetry",
        apiKey: "myapps",
        app: { name: "myapps-web", version: String(cfg.version || "dev") },
        sessionTracking: { enabled: true, persistent: true },
        ignoreUrls: [/\/api\/telemetry/, /\/assets\//, /\/icons\//],
        instrumentations: instrumentations
      });
      // How long until the tiles were on screen, from navigation.
      faro.api.pushMeasurement({ type: "apps_ready", values: { ready_ms: Math.round(performance.now()) } });
    } catch (e) {
      faro = null;
    }
  }

  // Viewing as someone else changes nothing: no events, and this
  // browser's favorites, hidden apps, and recents stay the admin's own.
  var viewing = !!cfg.viewing;

  function track(name, attrs) {
    if (faro && !viewing) faro.api.pushEvent(name, attrs);
  }

  // Kept in this browser only: the server never learns what anyone
  // favorited, hid, or opened lately.
  var store = {
    get: function (key) {
      try {
        var v = JSON.parse(localStorage.getItem(key) || "[]");
        return Array.isArray(v) ? v.filter(function (x) { return typeof x === "string"; }) : [];
      } catch (e) { return []; }
    },
    set: function (key, list) {
      if (viewing) return;
      try { localStorage.setItem(key, JSON.stringify(list)); } catch (e) { /* private mode: nothing kept */ }
    }
  };
  // Favorites keep the key they had when they were pins, so no one loses theirs.
  var RECENT = "myapps.recent", FAVS = "myapps.pins", HIDDEN = "myapps.hidden", RECENT_MAX = 8, RECENT_SHOWN = 6;
  var has = function (key, item) { return store.get(key).indexOf(item) !== -1; };
  // Adds the item when it is missing and drops it when it is there; says
  // whether it is in the list now.
  var toggleIn = function (key, item) {
    var list = store.get(key);
    var at = list.indexOf(item);
    if (at === -1) list.push(item); else list.splice(at, 1);
    store.set(key, list);
    return at === -1;
  };
  var dropFrom = function (key, item) { store.set(key, store.get(key).filter(function (k) { return k !== item; })); };
  // Apps open in a new tab unless this browser turned that off, so Home
  // stays open as the place everything starts. The server draws every app
  // link with a new-tab target; this only takes it away, or puts it back.
  var NEWTAB = "myapps.newtab";
  var newTab = function () {
    try { return localStorage.getItem(NEWTAB) !== "off"; } catch (e) { return true; }
  };
  var aim = function (a) {
    if (newTab()) { a.target = "_blank"; a.rel = "noopener"; } else { a.removeAttribute("target"); a.removeAttribute("rel"); }
  };
  var aimAll = function () { all('a[href^="/open/"], a[data-m="open"]').forEach(aim); };
  aimAll();
  all("[data-newtab]").forEach(function (box) {
    box.checked = newTab();
    box.disabled = viewing;
    box.addEventListener("change", function () {
      if (viewing) return;
      try { localStorage.setItem(NEWTAB, box.checked ? "on" : "off"); } catch (e) { /* private mode: nothing kept */ }
      all("[data-newtab]").forEach(function (b) { b.checked = box.checked; });
      aimAll();
    });
  });
  var STAR = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="m12 3.6 2.6 5.3 5.8.8-4.2 4.1 1 5.8-5.2-2.7-5.2 2.7 1-5.8-4.2-4.1 5.8-.8z"/></svg>';
  var CROSS = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M7 7l10 10M17 7 7 17"/></svg>';

  // A logo that will not load gives way to the initial.
  var fallBack = function (img) {
    var mono = document.createElement("span");
    mono.className = "mono";
    mono.setAttribute("aria-hidden", "true");
    mono.textContent = img.getAttribute("data-initial") || "?";
    (img.closest("picture") || img).replaceWith(mono);
  };
  // A logo that is a bare glyph (see-through inside its corners) gets a plate
  // and an inset, so it sits inside its tile like the logos that bring
  // their own. The icons come from this portal, so the canvas may read them.
  var probe = document.createElement("canvas");
  probe.width = probe.height = 24;
  var pctx = probe.getContext && probe.getContext("2d", { willReadFrequently: true });
  var glyphs = {};
  var plate = function (img) {
    var src = img.currentSrc || img.src;
    var mark = function (on) { var l = img.closest(".logo"); if (l && on) l.classList.add("glyph"); };
    if (src in glyphs) { mark(glyphs[src]); return; }
    try {
      pctx.clearRect(0, 0, 24, 24);
      pctx.drawImage(img, 0, 0, 24, 24);
      // A tile is see-through only at its rounded corners, a few hundredths
      // of it; a glyph drawn on nothing is see-through well beyond that.
      var px = pctx.getImageData(0, 0, 24, 24).data, clear = 0;
      for (var i = 3; i < px.length; i += 4) if (px[i] < 64) clear++;
      glyphs[src] = clear / 576 > 0.14;
      mark(glyphs[src]);
    } catch (e) { /* unreadable: leave it as drawn */ }
  };
  all("img[data-initial]").forEach(function (img) {
    if (img.complete && !img.naturalWidth) { fallBack(img); return; }
    img.addEventListener("error", function () { fallBack(img); });
    if (!pctx) return;
    if (img.complete) plate(img); else img.addEventListener("load", function () { plate(img); });
  });

  // A tile opened: the event names the category, never the app, so its values
  // stay a fixed list. The link itself does the navigating, through /open.
  document.addEventListener("click", function (e) {
    var a = e.target.closest("a[data-key][data-category]");
    if (a) {
      track("tile.open", { capability: a.getAttribute("data-category") });
      var key = a.getAttribute("data-key");
      var recent = store.get(RECENT).filter(function (k) { return k !== key; });
      recent.unshift(key);
      store.set(RECENT, recent.slice(0, RECENT_MAX));
    }
    if (e.target.closest("a[data-action]")) track("action.open", {});
  });

  // An open row menu closes when anything else is clicked or another opens.
  document.addEventListener("click", function (e) {
    all("details.rowmenu[open]").forEach(function (d) { if (!d.contains(e.target)) d.open = false; });
  });

  // A click on a dialog's backdrop, outside its box, closes it. A click in
  // its own padding has the dialog as its target too, so the box decides.
  var outside = function (e, dlg) {
    if (e.target !== dlg) return false;
    var r = dlg.getBoundingClientRect();
    return e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom;
  };

  // Finding: one ranking for the launcher on Home and the palette on every
  // page. A title that starts with the words wins, then a word in the
  // title that does, then the words anywhere; recently used breaks ties.
  var rank = function (items, q) {
    q = q.trim().toLowerCase();
    var recent = store.get(RECENT);
    var scored = [];
    items.forEach(function (it, i) {
      var title = it.title.toLowerCase();
      var s = -1;
      if (!q) s = it.kind === "app" ? 1 : 0;
      else if (title.indexOf(q) === 0) s = 40;
      else if ((" " + title).indexOf(" " + q) !== -1) s = 30;
      else if (title.indexOf(q) !== -1) s = 20;
      else if ((it.more || "").toLowerCase().indexOf(q) !== -1) s = 10;
      if (s < 0) return;
      var r = it.key ? recent.indexOf(it.key) : -1;
      if (r !== -1) s += 5 - Math.min(4, r) / 2;
      scored.push({ it: it, s: s, i: i });
    });
    scored.sort(function (a, b) { return b.s - a.s || a.i - b.i; });
    return scored.map(function (x) { return x.it; });
  };

  var icons = {
    action: '<svg viewBox="0 0 24 24"><path d="M13 3.5 5.5 13.5h6l-1 7 7.5-10h-6z"/></svg>',
    page: '<svg viewBox="0 0 24 24"><path d="M5 12h14M13 6l6 6-6 6"/></svg>'
  };
  var kindWord = { app: "App", action: "Action", page: "Page" };

  // A result row. An app's logo is copied from a tile the server drew, so
  // the page never builds an icon's address itself.
  var optionOf = function (it, id) {
    var li = document.createElement("li");
    li.className = "opt";
    li.id = id;
    li.setAttribute("role", "option");
    li.setAttribute("aria-selected", "false");
    var art;
    if (it.art) {
      art = it.art.cloneNode(true);
    } else if (it.logo) {
      art = it.logo.cloneNode(true);
      art.className = "logo";
    } else {
      art = document.createElement("span");
      art.className = "oicon";
      art.innerHTML = icons[it.kind] || icons.page;
    }
    art.setAttribute("aria-hidden", "true");
    var text = document.createElement("span");
    text.className = "otext";
    var t = document.createElement("span");
    t.className = "otitle";
    t.textContent = it.title;
    text.appendChild(t);
    if (it.sub) {
      var s = document.createElement("span");
      s.className = "osub";
      s.textContent = it.sub;
      text.appendChild(s);
    }
    var k = document.createElement("span");
    k.className = "okind";
    k.textContent = kindWord[it.kind] || "";
    li.appendChild(art);
    li.appendChild(text);
    li.appendChild(k);
    return li;
  };

  // Going somewhere from a result: an app through /open, so it is counted.
  var go = function (it) {
    if (it.kind === "app") {
      track("tile.open", { capability: it.category || "other" });
      var recent = store.get(RECENT).filter(function (k) { return k !== it.key; });
      recent.unshift(it.key);
      store.set(RECENT, recent.slice(0, RECENT_MAX));
    }
    if (it.kind === "action") track("action.open", {});
    if (it.external || (it.kind === "app" && newTab())) window.open(it.url, "_blank", "noopener"); else location.href = it.url;
  };

  // A list of results driven by a text field: arrows move, Enter opens.
  var listbox = function (input, list, source, max, onEmpty) {
    var shown = [], at = -1;
    var mark = function (i) {
      at = i;
      Array.prototype.forEach.call(list.children, function (li, j) {
        if (li.getAttribute("role") === "option") li.setAttribute("aria-selected", j === at ? "true" : "false");
      });
      var cur = list.children[at];
      if (cur) { input.setAttribute("aria-activedescendant", cur.id); cur.scrollIntoView({ block: "nearest" }); }
      else input.removeAttribute("aria-activedescendant");
    };
    var draw = function () {
      var q = input.value;
      shown = rank(source(), q).slice(0, max);
      list.textContent = "";
      shown.forEach(function (it, i) { list.appendChild(optionOf(it, list.id + "-" + i)); });
      if (!shown.length) {
        var none = document.createElement("li");
        none.className = "onone";
        none.textContent = onEmpty;
        list.appendChild(none);
      }
      mark(shown.length ? 0 : -1);
    };
    input.addEventListener("keydown", function (e) {
      if (e.key === "ArrowDown" && shown.length) { e.preventDefault(); mark((at + 1) % shown.length); }
      else if (e.key === "ArrowUp" && shown.length) { e.preventDefault(); mark((at - 1 + shown.length) % shown.length); }
      else if (e.key === "Enter" && at >= 0 && shown[at]) { e.preventDefault(); go(shown[at]); }
    });
    list.addEventListener("mousemove", function (e) {
      var li = e.target.closest(".opt");
      if (li) mark(Array.prototype.indexOf.call(list.children, li));
    });
    list.addEventListener("mousedown", function (e) { e.preventDefault(); });
    list.addEventListener("click", function (e) {
      var li = e.target.closest(".opt");
      if (li) go(shown[Array.prototype.indexOf.call(list.children, li)]);
    });
    return draw;
  };

  // What the page already knows: the tiles and actions it drew.
  var pageItems = function () {
    var items = [];
    all(".category .tile[data-tile]").forEach(function (t) {
      var a = t.querySelector("a[data-key]");
      items.push({
        kind: "app", key: t.getAttribute("data-tile"), title: t.getAttribute("data-title"), sub: t.getAttribute("data-desc"),
        more: t.getAttribute("data-search"), url: a.getAttribute("href"), category: a.getAttribute("data-category"),
        logo: t.querySelector(".logo")
      });
    });
    all(".tile.action a[data-action]").forEach(function (a) {
      items.push({
        kind: "action", title: a.querySelector(".atitle").textContent, sub: a.querySelector(".ameta").textContent,
        more: a.closest(".tile").getAttribute("data-search"), url: a.getAttribute("href"), external: true,
        art: a.querySelector(".aicon")
      });
    });
    return items;
  };

  // The server draws times in UTC; each is shown in the person's own clock.
  all("time[data-local]").forEach(function (t) {
    var d = new Date(t.getAttribute("datetime"));
    if (isNaN(d.getTime())) return;
    t.textContent = t.getAttribute("data-local") === "day"
      ? d.toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" })
      : d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  });

  // The greeting follows the person's own clock.
  var greet = document.querySelector("[data-greet]");
  if (greet) {
    var h = new Date().getHours();
    greet.textContent = h < 5 ? "Good evening" : h < 12 ? "Good morning" : h < 18 ? "Good afternoon" : "Good evening";
  }

  var search = document.getElementById("search");
  if (search) {
    // The tiles the server drew: one per app the person may see, once per
    // capability it serves. The dock copies from these, so a stored key
    // that is no longer visible draws nothing.
    var home = all(".category .tile[data-tile]");
    var byKey = {};
    home.forEach(function (t) { var k = t.getAttribute("data-tile"); if (!byKey[k]) byKey[k] = t; });
    var menu = document.getElementById("tile-menu");
    var dock = document.getElementById("dock");
    var shelf = document.getElementById("hidden-apps");
    var hiddenLink = document.querySelector("[data-hidden-link]");
    var count = document.getElementById("count");
    var hiddenKeys = function () { return store.get(HIDDEN).filter(function (k) { return byKey[k]; }); };
    var titleOf = function (k) { return byKey[k].getAttribute("data-title"); };
    var plural = function (n) { return n + (n === 1 ? " app" : " apps"); };
    // The apps Home shows: every one the person may open and has not hidden.
    var shownKeys = function () {
      var hidden = hiddenKeys();
      return Object.keys(byKey).filter(function (k) { return hidden.indexOf(k) === -1; });
    };

    var syncFavs = function () {
      var favs = store.get(FAVS);
      all("[data-fav]").forEach(function (b) {
        var tile = b.closest("[data-tile]");
        var on = favs.indexOf(tile.getAttribute("data-tile")) !== -1;
        var title = tile.getAttribute("data-title");
        b.setAttribute("aria-pressed", on ? "true" : "false");
        b.setAttribute("aria-label", on ? "Remove " + title + " from favorites" : "Add " + title + " to favorites");
        b.title = on ? "Remove from favorites" : "Favorite";
      });
    };

    // Hidden apps leave the groups and the minimap, and a group or a
    // capability with nothing left goes with them. Counts follow.
    var applyHidden = function () {
      var hidden = hiddenKeys();
      var gone = function (k) { return hidden.indexOf(k) !== -1; };
      home.forEach(function (t) { t.hidden = gone(t.getAttribute("data-tile")); });
      all(".category[data-category]:not(.acts)").forEach(function (sec) {
        var n = 0, head = null, headN = 0;
        var close = function () {
          if (!head) return;
          head.hidden = !headN;
          head.querySelector(".dim").textContent = headN;
        };
        Array.prototype.forEach.call(sec.querySelector(".grid").children, function (el) {
          if (el.classList.contains("subcap")) { close(); head = el; headN = 0; return; }
          if (el.hasAttribute("data-tile") && !el.hidden) { n++; headN++; }
        });
        close();
        sec.hidden = !n;
        sec.querySelector("h2 > .dim").textContent = n;
      });
      all(".minimap .mm").forEach(function (mm) {
        var n = 0;
        all(".logo.mini[data-key]", mm).forEach(function (l) {
          l.hidden = gone(l.getAttribute("data-key"));
          if (!l.hidden) n++;
        });
        all(".mmsub", mm).forEach(function (sub) { sub.hidden = !sub.querySelector(".logo.mini:not([hidden])"); });
        mm.querySelector(".mmn").textContent = n;
        mm.style.setProperty("--n", String(n));
        mm.hidden = !n;
      });
      if (count && !search.value.trim()) count.textContent = plural(shownKeys().length);
      if (hiddenLink) {
        hiddenLink.hidden = !hidden.length;
        hiddenLink.textContent = hidden.length + " hidden";
      }
      if (!shelf) return;
      var list = shelf.querySelector("[data-hidden-list]");
      list.textContent = "";
      hidden.forEach(function (k) {
        var li = document.createElement("li");
        li.className = "hrow";
        li.appendChild(byKey[k].querySelector(".logo").cloneNode(true));
        var name = document.createElement("span");
        name.className = "hname";
        name.textContent = titleOf(k);
        li.appendChild(name);
        var b = document.createElement("button");
        b.type = "button";
        b.className = "button ghost small";
        b.setAttribute("data-restore", k);
        b.setAttribute("aria-label", "Restore " + titleOf(k));
        b.textContent = "Restore";
        li.appendChild(b);
        list.appendChild(li);
      });
      shelf.querySelector("[data-hidden-count]").textContent = hidden.length;
      shelf.querySelector("[data-restore-all]").hidden = hidden.length < 2;
      shelf.hidden = !hidden.length;
      if (!hidden.length) shelf.open = false;
    };

    // One app in the dock: its logo and name, a star on a favorite, and a
    // button to forget a recent one. The button sits beside the link, never
    // inside it.
    var slotOf = function (k, kind) {
      var t = byKey[k];
      var src = t.querySelector("a[data-key]");
      var slot = document.createElement("div");
      slot.className = "dslot";
      slot.setAttribute("data-dock-key", k);
      var a = document.createElement("a");
      a.className = "dtile";
      a.href = src.getAttribute("href");
      aim(a);
      a.setAttribute("data-key", k);
      a.setAttribute("data-category", src.getAttribute("data-category"));
      a.title = titleOf(k);
      a.appendChild(t.querySelector(".logo").cloneNode(true));
      var n = document.createElement("span");
      n.className = "dname";
      n.textContent = titleOf(k);
      a.appendChild(n);
      if (kind === "fav") {
        var star = document.createElement("span");
        star.className = "favmark";
        star.setAttribute("aria-label", "Favorite");
        star.innerHTML = STAR;
        a.appendChild(star);
      }
      slot.appendChild(a);
      if (kind === "recent") {
        var x = document.createElement("button");
        x.type = "button";
        x.className = "dx";
        x.setAttribute("data-unrecent", k);
        x.setAttribute("aria-label", "Remove " + titleOf(k) + " from recent");
        x.title = "Remove from recent";
        x.innerHTML = CROSS;
        slot.appendChild(x);
      }
      return slot;
    };

    // The dock: favorites, then recently used; on a first visit, the
    // critical and important apps, with a word on how to make it yours.
    // A hidden app stays out of both, and comes back with it restored.
    var drawDock = function () {
      if (!dock) return;
      var hidden = hiddenKeys();
      var ok = function (k) { return byKey[k] && hidden.indexOf(k) === -1; };
      var favs = store.get(FAVS).filter(ok);
      var recent = store.get(RECENT).filter(function (k) { return ok(k) && favs.indexOf(k) === -1; }).slice(0, RECENT_SHOWN);
      var fallback = !favs.length && !recent.length;
      if (fallback) favs = Object.keys(byKey).filter(function (k) { return ok(k) && byKey[k].hasAttribute("data-featured"); }).slice(0, 8);
      var fill = function (name, keys, kind) {
        var group = dock.querySelector('[data-dgroup="' + name + '"]');
        var row = group.querySelector("[data-dock]");
        row.textContent = "";
        keys.forEach(function (k) { row.appendChild(slotOf(k, kind)); });
        group.hidden = !keys.length;
      };
      fill("fav", favs, fallback ? "suggested" : "fav");
      fill("recent", recent, "recent");
      dock.querySelector("[data-dlabel]").textContent = fallback ? "Suggested" : "Favorites";
      dock.querySelector("[data-dock-hint]").hidden = !fallback || !favs.length;
      dock.hidden = !favs.length && !recent.length;
    };
    var drawShelves = function () { applyHidden(); drawDock(); syncFavs(); };

    // An action that acts on an app wears that app's logo, with a bolt.
    all(".tile.action").forEach(function (t) {
      var group = (t.getAttribute("data-group") || "").toLowerCase();
      var app = Object.keys(byKey).map(function (k) { return byKey[k]; }).filter(function (a) {
        return (a.getAttribute("data-title") || "").toLowerCase() === group;
      })[0];
      var icon = t.querySelector(".aicon");
      var bolt = icon.querySelector("svg");
      var logo;
      if (app) {
        logo = app.querySelector(".logo").cloneNode(true);
      } else {
        // An action on something with no tile wears its group's initial,
        // in the same shape, so every action reads the same way.
        var name = t.getAttribute("data-group") || "";
        if (!name) return;
        logo = document.createElement("span");
        logo.className = "logo";
        var mono = document.createElement("span");
        mono.className = "mono";
        mono.textContent = (name.match(/[\p{L}\p{N}]/u) || ["?"])[0].toUpperCase();
        logo.style.setProperty("--hue", String(Array.prototype.reduce.call(name, function (h, ch) { return (h * 31 + ch.charCodeAt(0)) % 360; }, 7)));
        logo.appendChild(mono);
      }
      icon.textContent = "";
      icon.appendChild(logo);
      var b = document.createElement("span");
      b.className = "bolt";
      b.appendChild(bolt);
      icon.appendChild(b);
    });
    drawShelves();

    var toggleFav = function (key) {
      var on = toggleIn(FAVS, key);
      track("apps.favorite", { on: on ? "true" : "false" });
      drawShelves();
    };
    var forgetRecent = function (key) {
      dropFrom(RECENT, key);
      track("apps.recent.remove", {});
      drawShelves();
    };

    // A short word after a hide, with a way back.
    var toast = document.getElementById("toast");
    var toastTimer = null, undo = null;
    var say = function (text, onUndo) {
      if (!toast) return;
      toast.querySelector("[data-toast-text]").textContent = text;
      undo = onUndo;
      toast.hidden = false;
      clearTimeout(toastTimer);
      toastTimer = setTimeout(function () { toast.hidden = true; undo = null; }, 6000);
    };
    var hide = function (key) {
      if (!has(HIDDEN, key)) toggleIn(HIDDEN, key);
      track("apps.hide", {});
      drawShelves();
      say(titleOf(key) + " is hidden. Find it under Hidden apps.", function () { restore([key]); });
    };
    var restore = function (keys) {
      store.set(HIDDEN, store.get(HIDDEN).filter(function (k) { return keys.indexOf(k) === -1; }));
      track("apps.restore", {});
      drawShelves();
    };

    // A tile's sheet: what it is, its status, open it, favorite it, hide it,
    // forget it from recent. It is how a
    // phone or a keyboard reaches everything a hover shows.
    var openMenu = function (tile) {
      var set = function (part, text) {
        var el = menu.querySelector('[data-m="' + part + '"]');
        el.textContent = text;
        return el;
      };
      set("title", tile.getAttribute("data-title"));
      var badge = tile.querySelector("[data-dot]");
      var status = badge.className.replace("badge", "").trim();
      set("status", badge.title);
      menu.querySelector('[data-m="dot"]').className = "dot " + status;
      set("desc", tile.getAttribute("data-desc")).hidden = !tile.getAttribute("data-desc");
      set("capname", tile.getAttribute("data-capname"));
      var kindKey = tile.getAttribute("data-kindkey") || "";
      menu.querySelectorAll('[data-m="kmark"] [data-k]').forEach(function (mark) { mark.hidden = mark.getAttribute("data-k") !== kindKey; });
      var owner = menu.querySelector('[data-m="owner"]');
      owner.textContent = tile.getAttribute("data-owner") || "Not set";
      owner.classList.toggle("dim", !tile.getAttribute("data-owner"));
      ["tier", "kind"].forEach(function (f) {
        var v = tile.getAttribute("data-" + f);
        set(f, v).hidden = !v;
        menu.querySelector('[data-m="' + f + '-l"]').hidden = !v;
      });
      set("host", tile.getAttribute("data-host"));
      // The app's own actions: those Orrery groups under its name.
      var title = (tile.getAttribute("data-title") || "").toLowerCase();
      var list = menu.querySelector('[data-m="actlist"]');
      list.textContent = "";
      all(".tile.action").forEach(function (t) {
        if ((t.getAttribute("data-group") || "").toLowerCase() !== title) return;
        var src = t.querySelector("a[data-action]");
        var li = document.createElement("li");
        var a = document.createElement("a");
        a.href = src.getAttribute("href");
        a.setAttribute("data-action", "");
        a.appendChild(src.querySelector(".aicon").cloneNode(true));
        var mt = document.createElement("span");
        mt.className = "mtitle";
        mt.textContent = src.querySelector(".atitle").textContent;
        a.appendChild(mt);
        li.appendChild(a);
        list.appendChild(li);
      });
      menu.querySelector('[data-m="acts"]').hidden = !list.children.length;
      var orrery = tile.getAttribute("data-orrery");
      menu.querySelector('[data-m="orrery"]').href = orrery || "/";
      menu.querySelector('[data-m="orrery-p"]').hidden = !orrery;
      var logo = menu.querySelector('[data-m="logo"]');
      logo.textContent = "";
      var src = tile.querySelector(".logo > picture, .logo > img, .logo > .mono");
      if (src) logo.appendChild(src.cloneNode(true));
      menu.style.setProperty("--hue", tile.style.getPropertyValue("--hue"));
      var link = tile.querySelector("a[data-key]");
      var open = menu.querySelector('[data-m="open"]');
      open.href = link.getAttribute("href");
      open.textContent = "Open " + tile.getAttribute("data-title");
      open.setAttribute("data-key", link.getAttribute("data-key"));
      open.setAttribute("data-category", link.getAttribute("data-category"));
      var key = tile.getAttribute("data-tile");
      var fav = menu.querySelector('[data-m="fav"]');
      var faved = has(FAVS, key);
      fav.textContent = faved ? "Remove from favorites" : "Add to favorites";
      fav.setAttribute("aria-pressed", faved ? "true" : "false");
      menu.querySelector('[data-m="hide"]').hidden = has(HIDDEN, key);
      menu.querySelector('[data-m="unrecent"]').hidden = !has(RECENT, key);
      menu.setAttribute("data-for", key);
      menu.showModal();
    };

    document.addEventListener("click", function (e) {
      var fav = e.target.closest("[data-fav]");
      if (fav) { toggleFav(fav.closest("[data-tile]").getAttribute("data-tile")); return; }
      var x = e.target.closest("[data-unrecent]");
      if (x) { forgetRecent(x.getAttribute("data-unrecent")); return; }
      var back = e.target.closest("[data-restore]");
      if (back) { restore([back.getAttribute("data-restore")]); return; }
      if (e.target.closest("[data-restore-all]")) { restore(hiddenKeys()); return; }
      if (e.target.closest("[data-toast-undo]")) {
        if (undo) undo();
        undo = null;
        toast.hidden = true;
        return;
      }
      if (e.target.closest("[data-hidden-link]") && shelf) { shelf.open = true; return; }
      var more = e.target.closest("[data-more]");
      if (more && menu) { openMenu(more.closest("[data-tile]")); return; }
      if (menu && menu.open) {
        var key = menu.getAttribute("data-for");
        if (e.target.closest('[data-m="fav"]')) { toggleFav(key); menu.close(); return; }
        if (e.target.closest('[data-m="hide"]')) { menu.close(); hide(key); return; }
        if (e.target.closest('[data-m="unrecent"]')) { forgetRecent(key); menu.close(); return; }
      }
      if (outside(e, menu)) menu.close();
    });

    // On a touch screen a long press opens a tile's sheet, from its row or
    // from the dock, and the tap that ends the press does not open the app.
    if (menu && window.matchMedia("(hover: none)").matches) {
      var press = null, pressed = false;
      var cancel = function () { clearTimeout(press); press = null; };
      var pressedTile = function (el) {
        var slot = el.closest("[data-dock-key]");
        if (slot && !el.closest("[data-unrecent]")) return byKey[slot.getAttribute("data-dock-key")];
        return el.closest("[data-tile]");
      };
      document.addEventListener("touchstart", function (e) {
        var tile = pressedTile(e.target);
        pressed = false;
        if (!tile) return;
        press = setTimeout(function () { pressed = true; openMenu(tile); }, 450);
      }, { passive: true });
      document.addEventListener("touchmove", cancel, { passive: true });
      document.addEventListener("touchend", cancel);
      document.addEventListener("click", function (e) {
        if (pressed && pressedTile(e.target)) { e.preventDefault(); e.stopPropagation(); pressed = false; }
      }, true);
      document.addEventListener("contextmenu", function (e) {
        if (pressedTile(e.target)) e.preventDefault();
      });
    }

    // The launcher: typing ranks the best matches under the field and
    // filters the page below; Enter opens the first.
    var llist = document.getElementById("launch-list");
    var drawLaunch = listbox(search, llist, pageItems, 6, "Nothing you may open matches. Try the Landscape.");
    var searched = false;
    var openList = function (on) {
      llist.hidden = !on;
      search.setAttribute("aria-expanded", on ? "true" : "false");
    };
    // The list under the field is the one answer while typing; the page
    // below stays put, so nothing moves behind it.
    search.addEventListener("input", function () {
      var q = search.value.trim().toLowerCase();
      var keys = shownKeys();
      var shown = keys.filter(function (k) { return byKey[k].getAttribute("data-search").toLowerCase().indexOf(q) !== -1; }).length;
      if (count) count.textContent = q ? shown + " of " + keys.length : plural(keys.length);
      if (q) { drawLaunch(); openList(true); } else openList(false);
      if (q && !searched) { searched = true; track("apps.search", {}); }
      if (!q) searched = false;
    });
    search.addEventListener("keydown", function (e) {
      if (e.key === "Escape") { search.value = ""; search.dispatchEvent(new Event("input")); }
    });
    search.addEventListener("blur", function () { openList(false); });
    search.addEventListener("focus", function () { if (search.value.trim()) openList(true); });
    if (!("ontouchstart" in window)) search.focus();
  }

  // The palette, on every page: ⌘K or Ctrl K, or / when not typing. On
  // Home it is the launcher; elsewhere it reads the apps once from /v1/apps.
  var palette = document.getElementById("palette");
  if (palette && palette.showModal) {
    var pq = document.getElementById("palette-q");
    var plist = document.getElementById("palette-list");
    var pages = [];
    try { pages = JSON.parse(document.getElementById("pages-data").textContent); } catch (e) { pages = []; }
    var fetched = null;
    var paletteItems = function () {
      var items = search ? pageItems() : (fetched || []);
      return items.concat(pages.map(function (p) { return { kind: "page", title: p.title, url: p.url }; }));
    };
    var drawPalette = listbox(pq, plist, paletteItems, 30, "Nothing matches that.");
    var mac = /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);
    all("[data-mod]").forEach(function (k) { k.textContent = mac ? "⌘K" : "Ctrl K"; });
    var load = function () {
      if (search || fetched) return;
      fetch("/v1/apps", { headers: { Accept: "application/json" }, credentials: "same-origin" })
        .then(function (r) { return r.ok ? r.json() : null; })
        .then(function (data) {
          if (!data) return;
          var first = {};
          (data.groups || []).forEach(function (g) { (g.apps || []).forEach(function (a) { first[a.key] = g.category.key; }); });
          fetched = data.apps.map(function (a) {
            var logo = document.createElement("span");
            logo.className = "logo";
            var mono = document.createElement("span");
            mono.className = "mono";
            mono.textContent = (a.title.match(/[\p{L}\p{N}]/u) || ["?"])[0].toUpperCase();
            logo.appendChild(mono);
            return { kind: "app", key: a.key, title: a.title, sub: a.description, more: a.description, url: "/open/" + encodeURIComponent(a.key), category: first[a.key], logo: logo };
          }).concat((data.actions || []).map(function (x) {
            return { kind: "action", title: x.title, sub: x.group || "Self-service", more: x.description, url: x.url, external: true };
          }));
          if (palette.open) drawPalette();
        })
        .catch(function () {});
    };
    var openPalette = function () {
      if (palette.open) return;
      pq.value = "";
      load();
      drawPalette();
      palette.showModal();
      pq.focus();
      track("palette.open", {});
    };
    pq.addEventListener("input", drawPalette);
    palette.addEventListener("click", function (e) { if (outside(e, palette)) palette.close(); });
    all("[data-palette]").forEach(function (b) { b.addEventListener("click", openPalette); });
    document.addEventListener("keydown", function (e) {
      var typing = /^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement.tagName) || document.activeElement.isContentEditable;
      if ((e.key === "k" || e.key === "K") && (e.metaKey || e.ctrlKey)) { e.preventDefault(); openPalette(); }
      else if (e.key === "/" && !typing && !document.querySelector("dialog[open]")) { e.preventDefault(); openPalette(); }
    });
  }

  // A folded list unfolds once asked.
  all("[data-unfold]").forEach(function (b) {
    b.addEventListener("click", function () {
      document.getElementById(b.getAttribute("data-unfold")).classList.remove("folded");
      b.remove();
    });
  });

  // The account menu closes when anything else is clicked.
  document.addEventListener("click", function (e) {
    all("details.me[open]").forEach(function (d) { if (!d.contains(e.target)) d.open = false; });
  });

  // A wall card's tip: its name and kinds, on hover and on focus, placed
  // above the card and kept on screen. A small card shows nothing else.
  if (document.querySelector(".lcard[data-tip]")) {
    var tip = document.createElement("div");
    tip.className = "ltip";
    tip.setAttribute("aria-hidden", "true");
    tip.hidden = true;
    document.body.appendChild(tip);
    var tipFor = null;
    var showTip = function (card) {
      var name = card.querySelector(".lname");
      tip.textContent = "";
      var b = document.createElement("b");
      b.textContent = name ? name.textContent : "";
      var meta = document.createElement("span");
      meta.textContent = card.getAttribute("data-tip");
      tip.appendChild(b);
      tip.appendChild(meta);
      tip.hidden = false;
      tipFor = card;
      var r = card.getBoundingClientRect(), t = tip.getBoundingClientRect();
      var left = Math.max(8, Math.min(window.innerWidth - t.width - 8, r.left + r.width / 2 - t.width / 2));
      var top = r.top - t.height - 8;
      if (top < 8) top = r.bottom + 8;
      tip.style.left = left + "px";
      tip.style.top = top + "px";
    };
    var hideTip = function (card) {
      if (!card || card === tipFor) { tip.hidden = true; tipFor = null; }
    };
    all(".lcard[data-tip]").forEach(function (card) {
      card.addEventListener("pointerenter", function (e) { if (e.pointerType !== "touch") showTip(card); });
      card.addEventListener("pointerleave", function () { hideTip(card); });
      card.addEventListener("focus", function () { if (card.matches(":focus-visible")) showTip(card); });
      card.addEventListener("blur", function () { hideTip(card); });
      card.addEventListener("click", function () { hideTip(); });
    });
    window.addEventListener("scroll", function () { hideTip(); }, { passive: true });
  }

  // The landscape: the grid and matrix tabs, the filters, zoom, and a
  // panel per card. Event values come only from the lists the server drew,
  // so they stay a fixed set.
  var landscape = document.getElementById("landscape");
  if (landscape) {
    var lsearch = document.getElementById("lsearch");
    var selects = all("select[data-filter]");
    var cards = all(".lcard");
    var rows = all(".mrow");
    var boxes = all(".lbox");
    var categoryBoxes = all(".lcategory");
    var groups = all(".matrix tbody");
    var totals = all(".matrix tfoot .total");
    var lcount = document.getElementById("lcount");
    var lnoMatch = document.getElementById("lno-match");
    var ltotal = Number(lcount.getAttribute("data-total"));
    var categoryKeys = categoryBoxes.map(function (c) { return c.getAttribute("data-category"); });
    var lsearched = false;

    var fixed = function (value, list) { return list.indexOf(value) !== -1 ? value : "other"; };
    var optionsOf = function (name) {
      var sel = document.querySelector('select[data-filter="' + name + '"]');
      return Array.prototype.map.call(sel.options, function (o) { return o.value || "all"; });
    };

    // A filter's attribute holds one key or several, spaced: a card has
    // every kind it layers, and matches any one of them.
    var matches = function (el, q, want) {
      if (q && el.getAttribute("data-search").toLowerCase().indexOf(q) === -1) return false;
      return Object.keys(want).every(function (k) {
        return !want[k] || (el.getAttribute("data-" + k) || "").split(" ").indexOf(want[k]) !== -1;
      });
    };

    // The analysis: its rows follow the filters, and its counts, ticks,
    // and radar points follow the rows. Every place a point may sit was
    // laid out by the server; the page only picks one.
    var arows = all(".arow");
    var anone = document.querySelector(".anone");
    var ccounts = all(".atable [data-ccount]");
    var mcounts = all(".atable [data-mcount]");
    var builtCount = document.querySelector("[data-built]");
    var tracksEl = document.getElementById("radar-tracks");
    var tracks = tracksEl ? JSON.parse(tracksEl.textContent) : [];
    var spokes = all(".radar .spoke");
    var area = document.querySelector("[data-area]");
    var ritems = all(".rlist li");
    var pnames = ritems.map(function (li) { return li.querySelector(".rname").textContent; });
    var known = mcounts.length && mcounts[0].textContent !== "?";
    var analyse = function (q, want) {
      if (!arows.length) return;
      var counts = ccounts.map(function () { return 0; });
      var names = ccounts.map(function () { return []; });
      var marks = mcounts.map(function () { return 0; });
      var shown = 0;
      arows.forEach(function (r) {
        r.hidden = !matches(r, q, want);
        if (r.hidden) return;
        shown++;
        var title = r.querySelector(".aname").textContent;
        all("[data-dot]", r).forEach(function (td, i) {
          if (td.getAttribute("data-dot") === "on") { counts[i]++; names[i].push(title); }
        });
        all("[data-im]", r).forEach(function (td, i) {
          if (td.getAttribute("data-im") === "on") marks[i]++;
        });
      });
      if (anone) anone.hidden = shown > 0;
      if (builtCount) builtCount.textContent = shown;
      ccounts.forEach(function (td, i) { td.textContent = counts[i]; });
      if (known) mcounts.forEach(function (td, i) { td.textContent = marks[i]; });
      var points = [];
      spokes.forEach(function (g, i) {
        var track = tracks[i] || [];
        var step = track[Math.min(counts[i], track.length - 1)];
        if (!step) return;
        points.push(step.point.x + "," + step.point.y);
        g.querySelector("[data-pt]").setAttribute("cx", step.point.x);
        g.querySelector("[data-pt]").setAttribute("cy", step.point.y);
        var hit = g.querySelector("[data-hit]");
        hit.setAttribute("cx", step.point.x);
        hit.setAttribute("cy", step.point.y);
        var num = g.querySelector("[data-num]");
        num.setAttribute("x", step.number.x);
        num.setAttribute("y", step.number.y);
        num.textContent = counts[i];
        var tip = pnames[i] + ": " + counts[i] + (counts[i] === 1 ? " app" : " apps") + (counts[i] ? ", " + names[i].join(", ") : "");
        hit.setAttribute("aria-label", tip);
        hit.querySelector("title").textContent = tip;
      });
      if (area && points.length) area.setAttribute("points", points.join(" "));
      ritems.forEach(function (li, i) {
        li.querySelector("[data-rcount]").textContent = counts[i];
        li.querySelector("[data-rapps]").textContent = counts[i] ? names[i].join(", ") : "No app shown names it.";
      });
    };

    // Pointing at a radar point or a category in the list shows that
    // category's apps; leaving clears it.
    var focusAxis = function (i) {
      spokes.forEach(function (g, j) { g.classList.toggle("on", j === i); });
      ritems.forEach(function (li, j) { li.classList.toggle("on", j === i); });
    };
    spokes.concat(ritems).forEach(function (el) {
      var i = Number(el.getAttribute("data-axis"));
      var target = el.querySelector("[data-hit]") || el;
      target.addEventListener("mouseenter", function () { focusAxis(i); });
      target.addEventListener("focus", function () { focusAxis(i); });
      target.addEventListener("mouseleave", function () { focusAxis(-1); });
      target.addEventListener("blur", function () { focusAxis(-1); });
    });

    var apply = function () {
      var q = lsearch.value.trim().toLowerCase();
      var want = {};
      selects.forEach(function (s) { want[s.getAttribute("data-filter")] = s.value; });
      var seen = {};
      cards.forEach(function (c) {
        c.hidden = !matches(c, q, want);
        if (!c.hidden) seen[c.getAttribute("data-card")] = true;
      });
      boxes.forEach(function (b) { b.hidden = !b.querySelector(".lcard:not([hidden])"); });
      var filtered = q || selects.some(function (s) { return s.value; });
      categoryBoxes.forEach(function (p) { p.hidden = filtered && !p.querySelector(".lcard:not([hidden])"); });
      var counts = totals.map(function () { return 0; });
      rows.forEach(function (r) {
        r.hidden = !matches(r, q, want);
        if (r.hidden) return;
        all("td", r).forEach(function (td, i) {
          if (td.getAttribute("data-mark") === "serves") counts[i]++;
        });
      });
      groups.forEach(function (g) { g.hidden = !g.querySelector(".mrow:not([hidden])"); });
      totals.forEach(function (t, i) {
        t.textContent = counts[i];
        t.classList.toggle("thin", counts[i] < 2);
      });
      analyse(q, want);
      var shown = Object.keys(seen).length;
      lcount.textContent = filtered ? shown + " of " + ltotal : ltotal + (ltotal === 1 ? " card" : " cards");
      lnoMatch.hidden = shown > 0;
    };

    lsearch.addEventListener("input", function () {
      apply();
      var q = lsearch.value.trim();
      if (q && !lsearched) { lsearched = true; track("landscape.filter", { filter: "search", value: "text" }); }
      if (!q) lsearched = false;
    });
    selects.forEach(function (s) {
      s.addEventListener("change", function () {
        apply();
        var name = s.getAttribute("data-filter");
        track("landscape.filter", { filter: name, value: fixed(s.value || "all", optionsOf(name)) });
      });
    });

    // Zoom: the wall's cell size, in steps, kept in this browser.
    var wall = document.getElementById("view-grid");
    var steps = [40, 48, 56, 64, 72], ZOOM = "myapps.wallzoom";
    var zoomIn = all("[data-zoom]");
    var step = -1;
    try { step = steps.indexOf(Number(localStorage.getItem(ZOOM))); } catch (e) { /* nothing kept */ }
    if (step === -1) step = 1;
    var zoom = function (to) {
      step = Math.max(0, Math.min(steps.length - 1, to));
      wall.style.setProperty("--u", steps[step] + "px");
      zoomIn.forEach(function (b) {
        var d = Number(b.getAttribute("data-zoom"));
        b.disabled = (d < 0 && step === 0) || (d > 0 && step === steps.length - 1);
      });
      try { localStorage.setItem(ZOOM, String(steps[step])); } catch (e) { /* nothing kept */ }
    };
    zoom(step);
    zoomIn.forEach(function (b) {
      b.addEventListener("click", function () {
        var d = Number(b.getAttribute("data-zoom"));
        zoom(step + d);
        track("landscape.filter", { filter: "zoom", value: d < 0 ? "out" : "in" });
      });
    });

    // Tabs: the views, kept in the address so a reload stays put. The view
    // the server opened on needs no address of its own.
    var views = ["grid", "list", "matrix", "analysis"];
    var tabs = all("[data-view]");
    var first = (tabs.filter(function (t) { return t.getAttribute("aria-selected") === "true"; })[0] || tabs[0]);
    var opened = first ? first.getAttribute("data-view") : "grid";
    // A phone has no room for the matrix; the wall carries the same placing.
    var narrow = window.matchMedia("(max-width: 760px)").matches;
    if (narrow && opened === "matrix") opened = "grid";
    var zoomGroup = document.querySelector(".zoom");
    var show = function (view) {
      tabs.forEach(function (t) {
        var on = t.getAttribute("data-view") === view;
        t.setAttribute("aria-selected", on ? "true" : "false");
        document.getElementById(t.getAttribute("aria-controls")).hidden = !on;
      });
      if (zoomGroup) zoomGroup.hidden = view !== "grid";
    };
    tabs.forEach(function (t) {
      t.addEventListener("click", function () {
        var view = t.getAttribute("data-view");
        show(view);
        history.replaceState(null, "", location.pathname + location.search + (view === opened ? "" : "#" + view));
        track("landscape.filter", { filter: "view", value: fixed(view, views) });
      });
    });
    var hashView = location.hash.slice(1);
    if (narrow && hashView === "matrix") hashView = "grid";
    if (hashView !== opened && views.indexOf(hashView) !== -1 && document.getElementById("view-" + hashView)) show(hashView);
    else show(opened);

    // On a phone the filters fold behind one button.
    var ftoggle = document.querySelector("[data-filters]");
    if (ftoggle) ftoggle.addEventListener("click", function () {
      var open = landscape.classList.toggle("open");
      ftoggle.setAttribute("aria-expanded", open ? "true" : "false");
    });

    // A favorite from a card's panel sits at the top of Home.
    var favLabel = function (b) {
      var on = has(FAVS, b.getAttribute("data-fav-key"));
      b.setAttribute("aria-pressed", on ? "true" : "false");
      b.textContent = on ? "In favorites · Remove" : "Add to favorites";
    };
    all("[data-fav-key]").forEach(function (b) {
      favLabel(b);
      b.addEventListener("click", function () {
        var on = toggleIn(FAVS, b.getAttribute("data-fav-key"));
        track("apps.favorite", { on: on ? "true" : "false" });
        favLabel(b);
      });
    });

    // A card's panel opens from the card or its matrix row.
    document.addEventListener("click", function (e) {
      var opener = e.target.closest("[data-card]");
      if (opener) {
        var panel = document.getElementById("detail-" + opener.getAttribute("data-card"));
        if (panel && panel.showModal) {
          panel.showModal();
          track("landscape.open", {
            category: fixed(opener.getAttribute("data-home"), categoryKeys),
            origin: fixed(opener.getAttribute("data-kind"), optionsOf("origin"))
          });
        }
        return;
      }
      // A click on the backdrop, outside the panel's box, closes it.
      if (e.target.tagName === "DIALOG" && outside(e, e.target)) e.target.close();
    });

      // Status badges follow the catalog while the page is open.
    var ltext = { green: "Healthy", amber: "Needs attention", red: "Down or failing", grey: "Status unknown" };
    var lword = { green: "Healthy", amber: "Degraded", red: "Down", grey: "Unknown" };
    setInterval(function () {
      if (document.visibilityState !== "visible") return;
      fetch("/v1/landscape" + location.search, { headers: { Accept: "application/json" }, credentials: "same-origin" })
        .then(function (r) { return r.ok ? r.json() : null; })
        .then(function (data) {
          if (!data) return;
          data.cards.forEach(function (c) {
            all('.lcard[data-card="' + CSS.escape(c.key) + '"] [data-pdot]').forEach(function (dot) {
              dot.className = "lstatus " + c.status;
              dot.title = ltext[c.status] + (c.reasons && c.reasons.length ? ": " + c.reasons.join("; ") : "");
              dot.textContent = lword[c.status] || "Unknown";
            });
          });
        })
        .catch(function () {});
    }, 60000);
  }

  // The admin page: pick several suggestions for one decision.
  all("form.bulk").forEach(function (form) {
    var key = form.id.replace(/^bulk-/, "");
    var picks = all('[data-pick="' + key + '"]');
    var every = form.querySelector("[data-all]");
    var buttons = all("[data-bulk]", form);
    function update() {
      var n = picks.filter(function (p) { return p.checked; }).length;
      buttons.forEach(function (b) { b.disabled = n === 0; });
      if (every) {
        every.checked = n > 0 && n === picks.length;
        every.indeterminate = n > 0 && n < picks.length;
      }
    }
    picks.forEach(function (p) { p.addEventListener("change", update); });
    if (every) every.addEventListener("change", function () {
      picks.forEach(function (p) { if (!p.closest(".qrow").hidden) p.checked = every.checked; });
      update();
    });
    update();
  });

  // Status badges follow the catalog while the page is open.
  var banner = document.getElementById("catalog-banner");
  function refresh() {
    if (document.visibilityState !== "visible") return;
    fetch("/v1/apps", { headers: { Accept: "application/json" }, credentials: "same-origin" })
      .then(function (r) { return r.ok ? r.json() : null; })
      .then(function (data) {
        if (!data) return;
        var text = { green: "Healthy", amber: "Needs attention", red: "Down or failing", grey: "Status unknown" };
        var word = { green: "Healthy", amber: "Degraded", red: "Down", grey: "Unknown" };
        data.apps.forEach(function (app) {
          all('[data-key="' + CSS.escape(app.key) + '"] [data-dot]').forEach(function (dot) {
            dot.className = "badge " + app.status;
            dot.title = text[app.status] + (app.reasons && app.reasons.length ? ": " + app.reasons.join("; ") : "");
            dot.textContent = word[app.status] || "Unknown";
          });
        });
        if (banner) banner.hidden = !data.catalog.stale;
      })
      .catch(function () {});
  }
  if (document.querySelector("[data-dot]")) setInterval(refresh, 60000);
})();
