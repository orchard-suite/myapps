// The admin grid: every application a row, every field a column, each cell
// edited where it is. The server draws the columns, their choices, and each
// row; this lays them out, sorts and filters them, and sends a changed cell
// back as a form with only that field. Changes go one at a time, in order,
// so each starts from the one before.
(function () {
  "use strict";
  var root = document.getElementById("grid");
  var source = document.getElementById("grid-data");
  if (!root || !source) return;
  var data = JSON.parse(source.textContent);
  var cols = data.columns;
  var rows = data.rows;
  var writable = data.writable;
  var byId = {};
  cols.forEach(function (c) { byId[c.id] = c; });

  var tools = document.getElementById("grid-tools");
  var search = document.getElementById("grid-search");
  var count = document.getElementById("grid-count");
  var clear = document.getElementById("grid-clear");
  var colMenu = document.getElementById("grid-columns");
  var status = document.getElementById("grid-status");
  var addForm = document.getElementById("grid-add");
  var heading = document.getElementById("apps-count");
  tools.hidden = false;

  // Which columns show is this person's, kept in this browser only.
  var HIDDEN = "myapps.admin.hidden";
  var hidden = {};
  var kept = null;
  try { kept = JSON.parse(localStorage.getItem(HIDDEN) || "null"); } catch (e) { kept = null; }
  cols.forEach(function (c) { hidden[c.id] = kept ? kept.indexOf(c.id) >= 0 : !c.shown; });
  hidden.name = false;
  function keepHidden() {
    try { localStorage.setItem(HIDDEN, JSON.stringify(cols.filter(function (c) { return hidden[c.id]; }).map(function (c) { return c.id; }))); } catch (e) { /* kept for this visit only */ }
  }

  var sort = { id: "", dir: 1 };
  var filters = {}; // column id: {value: true}; "" is an empty cell
  var query = "";

  // Small DOM helpers. Text always goes in as text.
  function el(tag, props, kids) {
    var n = document.createElement(tag);
    if (props) Object.keys(props).forEach(function (k) {
      var v = props[k];
      if (v === undefined || v === null || v === false) return;
      if (k === "text") n.textContent = v;
      else if (k === "class") n.className = v;
      else if (k.slice(0, 2) === "on") n.addEventListener(k.slice(2), v);
      else n.setAttribute(k, v === true ? "" : v);
    });
    (kids || []).forEach(function (kid) { if (kid) n.appendChild(typeof kid === "string" ? document.createTextNode(kid) : kid); });
    return n;
  }
  function optionsOf(col, row) { return (col.perRow ? (row.options || {})[col.id] : col.options) || []; }
  function nameOf(col, row, key) {
    var o = optionsOf(col, row).filter(function (x) { return x.key === key; })[0];
    return o ? o.name : key;
  }
  function valuesOf(row, col) { return row.values[col.id] || []; }
  function host(raw) { try { return new URL(raw).host; } catch (e) { return raw; } }
  // The words a cell shows, for sorting, searching, and the value lists.
  function words(col, row) {
    var v = valuesOf(row, col);
    if (col.kind === "one" || col.kind === "many" || col.kind === "toggle") return v.map(function (k) { return nameOf(col, row, k); });
    return v.slice();
  }
  // A phone keeps the name column narrow, so the values have room.
  var narrow = window.matchMedia("(max-width: 760px)");
  function widthOf(c) { return c.id === "name" && narrow.matches ? 170 : c.width; }
  narrow.addEventListener("change", function () { draw(); });
  function shownCols() { return cols.filter(function (c) { return !hidden[c.id]; }); }

  // Status line: what was saved, or why not.
  function say(text, bad) {
    status.textContent = text;
    status.className = "gstatus" + (bad ? " bad" : "");
  }

  // ---- Drawing ----

  function logo(row) {
    var box = el("span", { class: "logo xs", style: "--hue:" + row.hue });
    if (row.logo) {
      var pic = el("picture");
      if (row.logoDark) pic.appendChild(el("source", { media: "(prefers-color-scheme: dark)", srcset: row.logoDark }));
      pic.appendChild(el("img", { src: row.logo, alt: "", width: 24, height: 24, decoding: "async" }));
      box.appendChild(pic);
    } else box.appendChild(el("span", { class: "mono", "aria-hidden": "true", text: row.initial }));
    return box;
  }
  function chip(text, cls) { return el("span", { class: "gchip" + (cls ? " " + cls : ""), text: text }); }
  function none(col) { return el("span", { class: "gnone", text: col.none || "" }); }

  function cellBody(col, row) {
    var v = valuesOf(row, col);
    switch (col.kind) {
      case "toggle":
        return el("span", { class: "gseg", role: "group", "aria-label": col.title }, col.options.map(function (o) {
          return el("button", {
            type: "button", "data-opt": o.key, "aria-pressed": v[0] === o.key ? "true" : "false", disabled: !writable, tabindex: "-1", text: o.name,
            onclick: function (e) { e.stopPropagation(); save(row, col, [o.key]); }
          });
        }));
      case "one":
        return v.length ? chip(nameOf(col, row, v[0]), col.id === "listed" ? "" : "k-" + v[0]) : none(col);
      case "many":
      case "list":
        if (!v.length) return none(col);
        var names = col.kind === "many" ? v.map(function (k) { return nameOf(col, row, k); }) : v;
        var box = el("span", { class: "gchips", title: names.join(", ") }, names.slice(0, 2).map(function (n) { return chip(n); }));
        if (names.length > 2) box.appendChild(el("span", { class: "gmore", text: "+" + (names.length - 2) }));
        return box;
      case "url":
        return v.length ? el("span", { class: "gtext", title: v[0], text: host(v[0]) }) : none(col);
      case "read":
        return el("span", { class: "gtext dim", text: v[0] || "" });
      default:
        if (col.id === "name") {
          var warn = row.warnings && row.warnings.length ? el("span", { class: "gwarn", title: row.warnings.join("\n"), "aria-label": row.warnings.join(". ") }) : null;
          return el("span", { class: "gname" }, [logo(row), el("span", { class: "gtext strong", text: v[0] || "" }), warn]);
        }
        return v.length ? el("span", { class: "gtext", title: v[0], text: v[0] }) : none(col);
    }
  }

  function editable(col) { return writable && col.kind !== "read" && col.kind !== "toggle"; }

  function cell(col, row) {
    var td = el("td", {
      class: "gc gc-" + col.kind + (col.id === "name" ? " gfix" : "") + (editable(col) ? " ged" : ""),
      "data-col": col.id, tabindex: "-1",
      "aria-label": col.id === "name" ? null : col.title
    }, [cellBody(col, row)]);
    return td;
  }

  function actions(row) {
    var b = el("button", { type: "button", class: "gact", "aria-label": "More for " + (row.values.name[0] || row.key), title: "More", tabindex: "-1", "aria-haspopup": "menu" }, [svg("M5 12h.01M12 12h.01M19 12h.01")]);
    b.addEventListener("click", function (e) { e.stopPropagation(); rowMenu(b, row); });
    return el("td", { class: "gc gacts" }, [b]);
  }

  function svg(d) {
    var ns = "http://www.w3.org/2000/svg";
    var s = document.createElementNS(ns, "svg");
    s.setAttribute("viewBox", "0 0 24 24");
    s.setAttribute("aria-hidden", "true");
    var p = document.createElementNS(ns, "path");
    p.setAttribute("d", d);
    s.appendChild(p);
    return s;
  }

  function rowEl(row) {
    var tr = el("tr", { "data-key": row.key });
    shownCols().forEach(function (c) { tr.appendChild(cell(c, row)); });
    tr.appendChild(actions(row));
    return tr;
  }

  function matches(row) {
    if (query) {
      var hay = shownCols().map(function (c) { return words(c, row).join(" "); }).join(" ").toLowerCase();
      if (hay.indexOf(query) < 0) return false;
    }
    return Object.keys(filters).every(function (id) {
      var f = filters[id];
      var col = byId[id];
      if (!col) return true;
      var v = valuesOf(row, col);
      if (!v.length) return !!f[""];
      return v.some(function (k) { return f[k]; });
    });
  }

  function ordered() {
    var list = rows.filter(matches);
    if (!sort.id) return list;
    var col = byId[sort.id];
    return list.slice().sort(function (a, b) {
      var x = words(col, a).join(", ").toLowerCase(), y = words(col, b).join(", ").toLowerCase();
      if (!x !== !y) return x ? -1 : 1; // empty cells last either way
      return x < y ? -sort.dir : x > y ? sort.dir : 0;
    });
  }

  var table, tbody;
  function draw() {
    var focus = focused();
    var shown = shownCols();
    table = el("table", { class: "dgrid", "aria-rowcount": rows.length + 1 });
    var cg = el("colgroup");
    shown.forEach(function (c) { cg.appendChild(el("col", { style: "width:" + widthOf(c) + "px" })); });
    cg.appendChild(el("col", { style: "width:44px" }));
    table.appendChild(cg);
    // A fixed layout takes its widths from the table's own width.
    table.style.width = shown.reduce(function (n, c) { return n + widthOf(c); }, 44) + "px";
    var hr = el("tr");
    shown.forEach(function (c) {
      var on = filters[c.id] ? Object.keys(filters[c.id]).length : 0;
      var b = el("button", { type: "button", class: "gh", title: c.hint || "", "aria-haspopup": "dialog", "aria-label": c.title + ": sort, filter, and every value" }, [
        el("span", { class: "ghl", text: c.title }),
        sort.id === c.id ? el("span", { class: "gsort", text: sort.dir > 0 ? "↑" : "↓", "aria-label": sort.dir > 0 ? "sorted A to Z" : "sorted Z to A" }) : null,
        on ? el("span", { class: "gfilt", text: String(on), title: on + " values picked" }) : null
      ]);
      b.addEventListener("click", function (e) { e.stopPropagation(); headMenu(b, c); });
      hr.appendChild(el("th", { class: c.id === "name" ? "gfix" : "", scope: "col", "aria-sort": sort.id === c.id ? (sort.dir > 0 ? "ascending" : "descending") : null }, [b]));
    });
    hr.appendChild(el("th", { class: "gacts", scope: "col" }, [el("span", { class: "sr", text: "Actions" })]));
    table.appendChild(el("thead", null, [hr]));
    tbody = el("tbody");
    var list = ordered();
    list.forEach(function (r) { tbody.appendChild(rowEl(r)); });
    table.appendChild(tbody);
    root.replaceChildren(table);
    if (!list.length) root.appendChild(el("p", { class: "gempty", text: rows.length ? "Nothing matches. Clear the filters to see every application." : "No applications yet. Make one from a suggestion above, or add one below." }));
    var filtered = query || Object.keys(filters).length;
    count.textContent = filtered ? list.length + " of " + rows.length : rows.length === 1 ? "1 application" : rows.length + " applications";
    clear.hidden = !filtered;
    if (heading) heading.textContent = rows.length;
    drawColumnMenu();
    var lead = tbody.querySelector("td.gc");
    if (lead) lead.tabIndex = 0;
    restore(focus);
  }

  function redrawRow(row) {
    var tr = tbody && tbody.querySelector('tr[data-key="' + CSS.escape(row.key) + '"]');
    if (!tr) return;
    var focus = focused();
    var fresh = rowEl(row);
    tr.replaceWith(fresh);
    restore(focus);
    return fresh;
  }

  function drawColumnMenu() {
    colMenu.replaceChildren.apply(colMenu, cols.map(function (c) {
      var box = el("input", { type: "checkbox", checked: !hidden[c.id], disabled: c.id === "name" });
      box.addEventListener("change", function () { hidden[c.id] = !box.checked; keepHidden(); draw(); });
      return el("label", { class: "gcol" }, [box, " " + c.title]);
    }));
  }

  // ---- Focus and the keyboard ----

  function focused() {
    var a = document.activeElement;
    var td = a && a.closest && a.closest("td.gc");
    if (!td || !root.contains(td)) return null;
    return { key: td.parentNode.getAttribute("data-key"), col: td.getAttribute("data-col") };
  }
  function cellAt(key, col) {
    return tbody && tbody.querySelector('tr[data-key="' + CSS.escape(key) + '"] td[data-col="' + CSS.escape(col) + '"]');
  }
  function restore(f) {
    if (!f) return;
    var td = cellAt(f.key, f.col);
    if (td) td.focus({ preventScroll: true });
  }
  function rowOf(td) {
    var key = td.parentNode.getAttribute("data-key");
    return rows.filter(function (r) { return r.key === key; })[0];
  }
  function move(td, dx, dy) {
    var tr = td.parentNode;
    var i = Array.prototype.indexOf.call(tr.children, td);
    var target = tr;
    if (dy) target = dy > 0 ? tr.nextElementSibling : tr.previousElementSibling;
    if (!target) return;
    var next = target.children[Math.max(0, Math.min(target.children.length - 2, i + dx))];
    if (next) { next.focus(); next.scrollIntoView({ block: "nearest", inline: "nearest" }); }
  }

  root.addEventListener("keydown", function (e) {
    var td = e.target.closest && e.target.closest("td.gc");
    if (!td || e.target !== td) return;
    var col = byId[td.getAttribute("data-col")];
    var keys = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] };
    if (keys[e.key]) { e.preventDefault(); move(td, keys[e.key][0], keys[e.key][1]); return; }
    if (!col) return;
    if ((e.key === "Enter" || e.key === "F2") && editable(col)) { e.preventDefault(); edit(td); return; }
    if (col.kind === "toggle" && writable && (e.key === "Enter" || e.key === " ")) {
      e.preventDefault();
      var row = rowOf(td);
      var at = col.options.map(function (o) { return o.key; }).indexOf(valuesOf(row, col)[0]);
      save(row, col, [col.options[(at + 1) % col.options.length].key]);
      return;
    }
    // Typing on a text cell starts its edit with what was typed.
    if (editable(col) && e.key.length === 1 && !e.metaKey && !e.ctrlKey && !e.altKey && /^(text|url|icon|list)$/.test(col.kind)) {
      e.preventDefault();
      edit(td, e.key);
    }
  });
  root.addEventListener("click", function (e) {
    var td = e.target.closest && e.target.closest("td.ged");
    if (!td || !root.contains(td) || td.querySelector("input")) return;
    if (pop && pop.anchor === td) { closePop(true); return; }
    edit(td);
  });

  // ---- Editing ----

  function edit(td, typed) {
    var row = rowOf(td);
    var col = byId[td.getAttribute("data-col")];
    if (!row || !col) return;
    if (col.kind === "one" || col.kind === "many") { pick(td, row, col); return; }
    var now = valuesOf(row, col);
    var input = el("input", {
      class: "gin", type: col.kind === "url" ? "url" : "text", value: col.kind === "list" ? now.join(", ") : now[0] || "",
      placeholder: col.kind === "url" ? "https://" : col.kind === "list" ? "With commas between" : col.none || "",
      maxlength: col.id === "description" ? 200 : 80, "aria-label": col.title, spellcheck: "false"
    });
    td.replaceChildren(input);
    td.classList.add("editing");
    input.focus();
    if (typed) input.value = typed; else input.select();
    var done = false;
    function finish(keep, then) {
      if (done) return;
      done = true;
      td.classList.remove("editing");
      var raw = input.value.trim();
      var next = col.kind === "list" ? raw.split(",").map(function (s) { return s.trim(); }).filter(Boolean) : raw ? [raw] : [];
      if (keep) save(row, col, next);
      else redrawRow(row);
      var at = cellAt(row.key, col.id);
      if (at) { at.focus(); if (then) then(at); }
    }
    input.addEventListener("keydown", function (e) {
      if (e.key === "Enter") { e.preventDefault(); finish(true, function (at) { move(at, 0, 1); }); }
      else if (e.key === "Escape") { e.preventDefault(); finish(false); }
      else if (e.key === "Tab") { e.preventDefault(); finish(true, function (at) { move(at, e.shiftKey ? -1 : 1, 0); }); }
    });
    input.addEventListener("blur", function () { finish(true); });
  }

  // A popover beside an element; one at a time, gone on Escape, on a click
  // elsewhere, or when the grid scrolls.
  var pop = null;
  function closePop(commit) {
    if (!pop) return;
    var p = pop;
    pop = null;
    p.node.remove();
    if (p.onClose) p.onClose(commit);
  }
  function openPop(anchor, node, onClose) {
    closePop(true);
    node.classList.add("gpop");
    document.body.appendChild(node);
    var r = anchor.getBoundingClientRect();
    var w = Math.max(r.width, 240);
    node.style.minWidth = w + "px";
    var h = node.offsetHeight;
    var left = Math.min(r.left, window.innerWidth - node.offsetWidth - 8);
    var below = window.innerHeight - r.bottom;
    node.style.left = Math.max(8, left) + "px";
    node.style.top = (below < h + 12 && r.top > below ? Math.max(8, r.top - h - 4) : r.bottom + 4) + "px";
    pop = { node: node, onClose: onClose, anchor: anchor };
  }
  document.addEventListener("mousedown", function (e) {
    if (pop && !pop.node.contains(e.target) && !pop.anchor.contains(e.target)) closePop(true);
  });
  document.addEventListener("keydown", function (e) {
    if (e.key === "Escape" && pop) { e.preventDefault(); var a = pop.anchor; closePop(false); if (a.focus) a.focus(); }
  });
  root.addEventListener("scroll", function () { closePop(true); }, { passive: true });
  window.addEventListener("resize", function () { closePop(true); });

  // A list of choices with a search when there are many, moved through with
  // the arrow keys.
  function choiceList(items, onPick, many) {
    var box = el("div", { class: "gchoices", role: "listbox", "aria-multiselectable": many ? "true" : null });
    var find = items.length > 7 ? el("input", { class: "gfind", type: "search", placeholder: "Find", "aria-label": "Find a choice", autocomplete: "off" }) : null;
    var list = el("div", { class: "glist" });
    var active = 0;
    var nodes = items.map(function (it) {
      var n = el("div", {
        class: "gopt" + (it.off ? " off" : "") + (it.none ? " gnoneopt" : ""), role: "option", "aria-selected": it.on ? "true" : "false", "aria-disabled": it.off ? "true" : null,
        style: it.depth ? "padding-left:" + (10 + it.depth * 16) + "px" : null
      }, [many && !it.none ? el("span", { class: "gbox", "aria-hidden": "true" }) : null, el("span", { text: it.name })]);
      n.addEventListener("mousedown", function (e) { e.preventDefault(); });
      n.addEventListener("click", function () { if (!it.off) onPick(it, n); });
      list.appendChild(n);
      return n;
    });
    function visible() { return nodes.filter(function (n) { return !n.hidden; }); }
    function mark(i) {
      var v = visible();
      if (!v.length) return;
      active = (i + v.length) % v.length;
      nodes.forEach(function (n) { n.classList.remove("active"); });
      v[active].classList.add("active");
      v[active].scrollIntoView({ block: "nearest" });
    }
    function key(e) {
      if (e.key === "ArrowDown") { e.preventDefault(); mark(active + 1); }
      else if (e.key === "ArrowUp") { e.preventDefault(); mark(active - 1); }
      else if (e.key === "Enter" || (e.key === " " && e.target !== find)) {
        var v = visible()[active];
        if (v) { e.preventDefault(); v.click(); }
      }
    }
    if (find) {
      find.addEventListener("input", function () {
        var q = find.value.trim().toLowerCase();
        nodes.forEach(function (n, i) { n.hidden = q && items[i].name.toLowerCase().indexOf(q) < 0; });
        mark(0);
      });
      box.appendChild(find);
    }
    box.appendChild(list);
    box.tabIndex = -1;
    box.addEventListener("keydown", key);
    var at = items.map(function (it) { return !!it.on && !it.none; }).indexOf(true);
    return { node: box, focus: function () { (find || box).focus(); mark(Math.max(0, at)); } };
  }

  function pick(td, row, col) {
    var now = valuesOf(row, col);
    var opts = optionsOf(col, row).slice();
    // A value the catalog no longer offers stays, so it can be taken off.
    now.forEach(function (k) { if (!opts.some(function (o) { return o.key === k; })) opts.push({ key: k, name: k }); });
    var many = col.kind === "many";
    if (!opts.length && many) { say(col.title + ": there is nothing to pick. " + (col.hint || ""), true); return; }
    var chosen = now.slice();
    var items = opts.map(function (o) { return { key: o.key, name: o.name, depth: o.depth, off: o.off, on: now.indexOf(o.key) >= 0 }; });
    if (!many) items.unshift({ key: "", name: col.none || "None", none: true, on: !now.length });
    var head = el("div", { class: "gphead" }, [el("strong", { text: col.title }), el("span", { class: "dim", text: row.values.name[0] || "" })]);
    var c = choiceList(items, function (it, n) {
      if (!many) { chosen = it.key ? [it.key] : []; closePop(true); td.focus(); return; }
      var i = chosen.indexOf(it.key);
      if (i >= 0) chosen.splice(i, 1); else chosen.push(it.key);
      n.setAttribute("aria-selected", i >= 0 ? "false" : "true");
    }, many);
    var kids = [head, c.node];
    if (many) {
      kids.push(el("div", { class: "gpfoot" }, [
        el("span", { class: "dim", text: col.id === "sign_in" ? "The first picked is the one a request grants." : "" }),
        el("button", { type: "button", class: "button small", text: "Done", onclick: function () { closePop(true); td.focus(); } })
      ]));
    }
    var node = el("div", { role: "dialog", "aria-label": col.title + " of " + (row.values.name[0] || row.key) }, kids);
    td.classList.add("editing");
    openPop(td, node, function (commit) {
      td.classList.remove("editing");
      if (commit) save(row, col, chosen);
    });
    c.focus();
  }

  // The header's menu: sort, hide, and every value the column holds, with
  // how many applications hold it, to filter by.
  function headMenu(btn, col) {
    var tally = {};
    var order = [];
    rows.forEach(function (r) {
      var v = valuesOf(r, col);
      (v.length ? v : [""]).forEach(function (k) {
        if (!(k in tally)) { tally[k] = 0; order.push(k); }
        tally[k]++;
      });
    });
    var rowFor = function (k) { return rows.filter(function (r) { return valuesOf(r, col).indexOf(k) >= 0; })[0]; };
    var label = function (k) { return k === "" ? (col.none || "Empty") : col.kind === "url" ? host(k) : (col.kind === "one" || col.kind === "many" || col.kind === "toggle") ? nameOf(col, rowFor(k), k) : k; };
    order.sort(function (a, b) {
      if (a === "" || b === "") return a === "" ? 1 : -1;
      return tally[b] - tally[a] || label(a).localeCompare(label(b));
    });
    var f = filters[col.id] || {};
    var items = order.map(function (k) { return { key: k, name: label(k), count: tally[k], on: !!f[k] }; });
    var c = choiceList(items, function (it, n) {
      var cur = filters[col.id] || {};
      if (cur[it.key]) delete cur[it.key]; else cur[it.key] = true;
      if (Object.keys(cur).length) filters[col.id] = cur; else delete filters[col.id];
      n.setAttribute("aria-selected", cur[it.key] ? "true" : "false");
      draw();
    }, true);
    c.node.querySelectorAll(".gopt").forEach(function (n, i) { n.appendChild(el("span", { class: "gcount", text: String(items[i].count) })); });
    var sorter = function (dir, text) {
      return el("button", { type: "button", class: "gmi" + (sort.id === col.id && sort.dir === dir ? " on" : ""), text: text, onclick: function () {
        sort = sort.id === col.id && sort.dir === dir ? { id: "", dir: 1 } : { id: col.id, dir: dir };
        closePop(false);
        draw();
      } });
    };
    var kids = [
      el("div", { class: "gphead" }, [el("strong", { text: col.title }), col.hint ? el("span", { class: "dim", text: col.hint }) : null]),
      el("div", { class: "gmenu" }, [sorter(1, "Sort A to Z"), sorter(-1, "Sort Z to A"),
        col.id === "name" ? null : el("button", { type: "button", class: "gmi", text: "Hide the column", onclick: function () { hidden[col.id] = true; keepHidden(); closePop(false); draw(); } })]),
      el("p", { class: "gplabel", text: order.length + (order.length === 1 ? " value" : " values") + ". Pick any to show only those." }),
      c.node
    ];
    if (filters[col.id]) kids.push(el("div", { class: "gpfoot" }, [el("button", { type: "button", class: "button ghost small", text: "Show every value", onclick: function () { delete filters[col.id]; closePop(false); draw(); } })]));
    openPop(btn, el("div", { role: "dialog", "aria-label": col.title + " values" }, kids));
    c.focus();
  }

  function rowMenu(btn, row) {
    var name = row.values.name[0] || row.key;
    var url = (row.values.url || [])[0];
    var kids = [el("div", { class: "gphead" }, [el("strong", { text: name }), el("span", { class: "dim", text: row.key })])];
    var menu = el("div", { class: "gmenu" });
    if (url) menu.appendChild(el("a", { class: "gmi", href: url, rel: "noopener", target: "_blank", text: "Open its address ↗" }));
    if (row.orrery) menu.appendChild(el("a", { class: "gmi", href: row.orrery, rel: "noopener", target: "_blank", text: "Open in Orrery ↗" }));
    if (writable) menu.appendChild(el("button", { type: "button", class: "gmi bad", text: "Remove the application", onclick: function () {
      closePop(false);
      if (!window.confirm("Remove " + name + "? Its tile and its Landscape card go with it.")) return;
      remove(row);
    } }));
    kids.push(menu);
    openPop(btn, el("div", { role: "menu", "aria-label": name }, kids));
    var first = menu.querySelector("a,button");
    if (first) first.focus();
  }

  // ---- Saving ----

  var queue = Promise.resolve();
  function post(path, form) {
    return fetch(path, { method: "POST", body: form, credentials: "same-origin", headers: { Accept: "application/json" } })
      .then(function (r) {
        return r.json().catch(function () { return {}; }).then(function (body) {
          if (!r.ok) throw new Error(body.error || "Not saved. Try again.");
          return body;
        });
      }, function () { throw new Error("Not saved. My Apps could not be reached. Try again."); });
  }
  function same(a, b) { return a.length === b.length && a.every(function (v, i) { return v === b[i]; }); }

  function save(row, col, next) {
    var before = valuesOf(row, col);
    if (same(before, next)) { redrawRow(row); return; }
    row.values[col.id] = next;
    var tr = redrawRow(row);
    var td = tr && tr.querySelector('td[data-col="' + CSS.escape(col.id) + '"]');
    if (td) td.classList.add("saving");
    var form = new URLSearchParams();
    if (next.length) next.forEach(function (v) { form.append(col.id, v); }); else form.append(col.id, "");
    var label = (row.values.name[0] || row.key);
    queue = queue.then(function () {
      return post("/admin/applications/" + encodeURIComponent(row.key), form).then(function (body) {
        var i = rows.indexOf(row);
        if (i >= 0 && body.row) rows[i] = body.row;
        redrawRow(rows[i] || row);
        say("Saved " + col.title.toLowerCase() + " of " + (body.row ? body.row.values.name[0] : label) + ".");
      }, function (err) {
        row.values[col.id] = before;
        var back = redrawRow(row);
        var c = back && back.querySelector('td[data-col="' + CSS.escape(col.id) + '"]');
        if (c) c.classList.add("failed");
        say(col.title + " of " + label + ": " + err.message, true);
      });
    });
  }

  function remove(row) {
    queue = queue.then(function () {
      return post("/admin/applications/" + encodeURIComponent(row.key) + "/delete", new URLSearchParams()).then(function () {
        rows = rows.filter(function (r) { return r !== row; });
        draw();
        say("Removed " + (row.values.name[0] || row.key) + ".");
      }, function (err) { say(err.message, true); });
    });
  }

  if (addForm) addForm.addEventListener("submit", function (e) {
    e.preventDefault();
    var input = addForm.querySelector("input[name=name]");
    var name = input.value.trim();
    if (!name) return;
    var button = addForm.querySelector("button");
    button.disabled = true;
    queue = queue.then(function () {
      return post(addForm.action, new URLSearchParams({ name: name })).then(function (body) {
        rows.push(body.row);
        input.value = "";
        draw();
        var tr = tbody.querySelector('tr[data-key="' + CSS.escape(body.row.key) + '"]');
        if (tr) { tr.classList.add("fresh"); tr.scrollIntoView({ block: "nearest" }); }
        say("Added " + name + ". Fill in its cells in the grid.");
      }, function (err) { say(err.message, true); }).then(function () { button.disabled = false; input.focus(); });
    });
  });

  search.addEventListener("input", function () { query = search.value.trim().toLowerCase(); draw(); });
  clear.addEventListener("click", function () { filters = {}; query = ""; search.value = ""; draw(); });

  draw();
})();
