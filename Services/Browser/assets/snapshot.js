// Argus snapshot pass. Runs once per observation, in an isolated execution
// world, and returns a JSON string describing everything the agent can act on.
//
// Two rules shape this file. First: nothing is knowable without descending into
// shadow roots, because a design-system button lives inside one and a
// querySelectorAll('*') never crosses a boundary. Second: the output has to be
// small enough to sit in a model's context every single step, so the pruning
// here is the product.
(function () {
  'use strict';

  var MAX_CANDIDATES = 4000;   // ceiling on the walk itself, not on the output
  var MAX_NAME = 140;
  var MAX_HINT = 90;

  var INTERACTIVE_TAGS = {
    A: 'link', BUTTON: 'button', INPUT: null, TEXTAREA: 'textbox', SELECT: 'combobox',
    SUMMARY: 'button', LABEL: null, OPTION: 'option', PROGRESS: 'progressbar',
    METER: 'meter', DETAILS: 'group', AUDIO: 'player', VIDEO: 'player'
  };

  var LANDMARK_TAGS = {
    MAIN: 'main', NAV: 'navigation', HEADER: 'banner', FOOTER: 'contentinfo',
    ASIDE: 'complementary', SECTION: 'region', ARTICLE: 'article', FORM: 'form',
    SEARCH: 'search', DIALOG: 'dialog'
  };

  var SKIP_TAGS = {
    SCRIPT: 1, STYLE: 1, NOSCRIPT: 1, TEMPLATE: 1, META: 1, LINK: 1, HEAD: 1, TITLE: 1
  };

  // Roles worth showing even though they are not clickable: they are the
  // structure the agent reads the page with.
  var STRUCTURAL = {
    heading: 1, main: 1, navigation: 1, article: 1, region: 1, banner: 1,
    contentinfo: 1, complementary: 1, search: 1, form: 1, dialog: 1, alertdialog: 1,
    status: 1, alert: 1, list: 1, listitem: 1, table: 1, grid: 1, textbox: 1,
    searchbox: 1, combobox: 1
  };

  // Landmarks: containers that hold other things, and take a name only from an
  // explicit label.
  var CONTAINER_ROLES = {
    main: 1, navigation: 1, banner: 1, contentinfo: 1, complementary: 1, region: 1,
    search: 1, form: 1, list: 1, table: 1, grid: 1, tablist: 1, menu: 1, menubar: 1,
    group: 1, radiogroup: 1, tree: 1, treegrid: 1, feed: 1, generic: 1, document: 1,
    application: 1, presentation: 1
  };

  function lower(s) { return (s || '').toLowerCase(); }

  function trimText(s) {
    if (!s) return '';
    return String(s).replace(/\s+/g, ' ').trim();
  }

  function clip(s, n) {
    s = s || '';
    return s.length > n ? s.slice(0, n - 1) + '…' : s;
  }

  // A human check is the one obstacle the agent must not try to walk around, and
  // the accessibility tree is blind to it: the widget lives in a cross-origin
  // frame, so its "I'm not a robot" text and its checkbox are in another realm
  // that this walk can enter but never read. What the embedding page does leak is
  // the scaffolding — the script and iframe addresses that name the vendor, the
  // class a site puts on the placeholder, and, for a full-page interstitial, the
  // heading it prints. Those are readable across the boundary because they are
  // plain attributes of the parent's own elements.
  //
  // This collects short evidence strings and stops there: naming reCAPTCHA is the
  // page's job, deciding what to do about it is the Go side's.
  var CHALLENGE_SRC = /(recaptcha|google-challenge|h-?captcha|turnstile|challenge-platform|arkose|funcaptcha|geo\.captcha|\/captcha\b|captcha\.(?:js|html|png))/i;
  var CHALLENGE_TEXT = /(verify you are(?: a)? human|prove you('|’)re(?: a)? human|i('|’)m not a robot|are you a robot|select all (?:the )?squares|choose all(?: the)? images|drag the|(?:just a|one) ?moment|attention required|checking your browser|security check|enter the code|one[- ]time (?:code|password)|access denied)/i;

  function challengeEvidence() {
    var out = [];
    function add(s) {
      s = clip(trimText(s), 90);
      if (!s) return;
      if (out.indexOf(s) < 0) out.push(s);
    }
    try {
      if (CHALLENGE_SRC.test(location.href)) add('url ' + location.href);

      var srcs = document.querySelectorAll('script[src], iframe[src], frame[src], link[href]');
      for (var i = 0; i < srcs.length && out.length < 4; i++) {
        var src = srcs[i].getAttribute('src') || srcs[i].getAttribute('href') || '';
        if (src && CHALLENGE_SRC.test(src)) add(lower(srcs[i].tagName) + ' ' + src);
      }

      var marks = document.querySelectorAll(
        '[class*="captcha" i], [id*="captcha" i], [class*="turnstile" i],' +
        '[id*="turnstile" i], [name*="captcha" i], [data-sitekey]');
      for (var j = 0; j < marks.length && j < 20 && out.length < 5; j++) {
        var cls = trimText(marks[j].className).split(' ')[0];
        var id = marks[j].id || '';
        // data-sitekey is the widget's own hook: reCAPTCHA, hCaptcha and Turnstile
        // all read it, and nothing else on a normal page uses that attribute name.
        if (marks[j].hasAttribute('data-sitekey')) {
          add(lower(marks[j].tagName) + '[data-sitekey]' + (cls ? ' ' + cls : ''));
          continue;
        }
        if (!/captcha|turnstile|challenge|arkose|funcaptcha/i.test(cls + ' ' + id)) continue;
        add(lower(marks[j].tagName) + '.' + (cls || id));
      }

      // The interstitial pages a bot check shows instead of a widget: a headline
      // in the page's own document. Bounded to the visible headings and first
      // paragraphs, because reading the whole body text on every snapshot would
      // cost more than the check is worth.
      if (document.title && CHALLENGE_TEXT.test(document.title)) add('title ' + document.title);
      var words = document.querySelectorAll('h1, h2, h3, p, label, [role="heading"]');
      for (var k = 0; k < words.length && k < 60 && out.length < 6; k++) {
        var t = trimText(words[k].textContent);
        if (!t) continue;
        var hit = t.match(CHALLENGE_TEXT);
        if (hit) add(lower(words[k].tagName) + ' "' + clip(hit[0], 60) + '"');
      }
    } catch (e) {
      // A page this hostile about its DOM is not worth failing a snapshot over.
    }
    return out.slice(0, 4);
  }

  // A node is rendered when it has a box and is not hidden by CSS, an attribute,
  // or clipping. Computed style is read inside each shadow root, never inherited
  // across a boundary, which is the difference between seeing a web component and
  // missing it entirely.
  function isVisible(el, doc) {
    var win = doc.defaultView || doc.parentView;
    if (!win) return false;
    var cs;
    try { cs = win.getComputedStyle(el); } catch (e) { return false; }
    if (!cs) return false;
    if (cs.display === 'none' || cs.visibility === 'hidden' || cs.visibility === 'collapse') return false;
    if (parseFloat(cs.opacity || '1') < 0.06) return false;
    if (el.hasAttribute('hidden') || el.getAttribute('aria-hidden') === 'true') return false;
    // Details content that is closed is not in the a11y tree.
    if (el.tagName === 'DETAILS' && !el.open) {
      var openChild = el.querySelector(':scope > :not(summary)');
      if (!openChild) return false;
    }
    var box;
    try { box = el.getBoundingClientRect(); } catch (e) { return false; }
    if (!box) return false;
    // Off-screen but scrollable content still counts: the agent can scroll to it.
    if (box.width < 2 || box.height < 2) {
      // Zero box with children that have boxes is a wrapper; keep it if a
      // role/name says otherwise.
      if (!el.getAttribute('role') && !isInteractive(el)) return false;
    }
    // Clipped away by an overflow container with no way to reach it.
    if (cs.clipPath && cs.clipPath !== 'none' && /inset\(0/.test(cs.clipPath) && !isInteractive(el)) return false;
    return true;
  }

  function isInteractive(el) {
    if (el.tabIndex >= 0 && el.tagName !== 'BODY' && el.tagName !== 'HTML') return true;
    var role = lower(el.getAttribute('role'));
    if (role && INTERACTIVE_ROLES[role]) return true;
    var tag = el.tagName;
    if (tag === 'A' && el.hasAttribute('href')) return true;
    if (tag === 'BUTTON' || tag === 'TEXTAREA' || tag === 'SELECT' || tag === 'SUMMARY') return true;
    if (tag === 'INPUT') return true;
    if (el.isContentEditable) return true;
    if (el.getAttribute('contenteditable') === 'true') return true;
    // Frameworks attach listeners without any attribute to read; an onclick or a
    // cursor that says pointer is the visible half of that.
    if (el.onclick || el.getAttribute('ng-click') || el.getAttribute('v-on:click') ||
        el.getAttribute('@click') || el.getAttribute('x-on:click')) return true;
    return false;
  }

  var INTERACTIVE_ROLES = {
    button: 1, link: 1, checkbox: 1, radio: 1, textbox: 1, searchbox: 1, combobox: 1,
    listbox: 1, menuitem: 1, menuitemcheckbox: 1, menuitemradio: 1, option: 1,
    switch: 1, tab: 1, slider: 1, spinbutton: 1, menu: 1, menubar: 1, treeitem: 1,
    gridcell: 1, row: 1, cell: 1, banner: 0
  };

  function inputRole(el) {
    var type = lower(el.getAttribute('type')) || 'text';
    switch (type) {
      case 'button': case 'submit': case 'reset': return 'button';
      case 'checkbox': return 'checkbox';
      case 'radio': return 'radio';
      case 'range': return 'slider';
      case 'number': return 'spinbutton';
      case 'email': case 'tel': case 'url': case 'password': case 'text': case 'search':
      case 'date': case 'datetime-local': case 'month': case 'time': case 'week':
        return el.getAttribute('type') === 'search' ? 'searchbox' : 'textbox';
      case 'file': return 'button';
      case 'color': return 'textbox';
      case 'hidden': return 'none';
      default: return 'textbox';
    }
  }

  // roleOf is a first guess. Where it is a guess, the Go side replaces it with
  // Chrome's own computed role, so an error here costs accuracy, not the feature.
  function roleOf(el) {
    // Any explicit ARIA role wins, and is taken verbatim rather than looked up in
    // a whitelist — the set of roles grows and an unlisted role is not a reason to
    // call a control generic.
    var raw = lower(el.getAttribute('role'));
    if (raw === 'presentation' || raw === 'none') return { role: 'none', guessed: false };
    if (raw) return { role: raw.replace(/[\s_]+/g, ' '), guessed: false };
    var tag = el.tagName;
    if (INTERACTIVE_TAGS[tag] !== undefined) {
      if (tag === 'INPUT') {
        var r = inputRole(el);
        return { role: r, guessed: r === 'none' };
      }
      if (tag === 'A') return { role: el.hasAttribute('href') ? 'link' : 'generic', guessed: false };
      if (tag === 'LABEL') return { role: 'generic', guessed: true };
      return { role: INTERACTIVE_TAGS[tag] || 'generic', guessed: false };
    }
    if (LANDMARK_TAGS[tag]) return { role: LANDMARK_TAGS[tag], guessed: tag === 'SECTION' || tag === 'ARTICLE' };
    if (/^H[1-6]$/.test(tag)) return { role: 'heading', guessed: false };
    if (tag === 'IMG') return { role: el.getAttribute('alt') ? 'img' : 'none', guessed: false };
    if (tag === 'SVG' || tag === 'CANVAS') return { role: 'img', guessed: true };
    if (tag === 'P') return { role: 'paragraph', guessed: false };
    if (tag === 'LI') return { role: 'listitem', guessed: false };
    if (tag === 'TR') return { role: 'row', guessed: false };
    if (tag === 'TD') return { role: 'cell', guessed: false };
    if (tag === 'TH') return { role: 'columnheader', guessed: false };
    return { role: 'generic', guessed: true };
  }

  // The ARIA name computation, in priority order. Text inside a control is its
  // name, which is why an icon button with a <span> child still reads correctly.
  function nameOf(el, doc) {
    var labelled = el.getAttribute('aria-labelledby');
    if (labelled) {
      var parts = [];
      var ids = labelled.split(/\s+/);
      for (var i = 0; i < ids.length; i++) {
        var target = findInScope(doc, ids[i]);
        if (target) parts.push(trimText(textOf(target)));
      }
      var joined = trimText(parts.join(' '));
      if (joined) return { name: joined, from: 'labelledby' };
    }
    var ariaLabel = trimText(el.getAttribute('aria-label'));
    if (ariaLabel) return { name: clip(ariaLabel, MAX_NAME), from: 'aria-label' };

    var tag = el.tagName;
    if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') {
      // Never serialize what the user typed. After a takeover this can be a
      // password, OTP, card number, email or other personal data; labels and
      // placeholders identify the control without sending its value upstream.
      var labelled = labelFor(el, doc);
      if (labelled) return { name: clip(labelled, MAX_NAME), from: 'label' };
      if (el.placeholder) return { name: clip(trimText(el.placeholder), MAX_NAME), from: 'placeholder' };
      if (el.name) return { name: clip(trimText(el.name), MAX_NAME), from: 'name' };
      if (tag === 'SELECT') {
        var opt = el.options && el.options[el.selectedIndex];
        if (opt) return { name: clip(trimText(opt.textContent), MAX_NAME), from: 'value' };
      }
      if (el.title) return { name: clip(trimText(el.title), MAX_NAME), from: 'title' };
      return { name: '', from: 'none' };
    }
    if (tag === 'IMG' || tag === 'AREA') {
      if (el.alt) return { name: clip(trimText(el.alt), MAX_NAME), from: 'alt' };
      if (el.title) return { name: clip(trimText(el.title), MAX_NAME), from: 'title' };
    }

    var own = trimText(textOf(el));
    if (own) return { name: clip(own, MAX_NAME), from: 'contents' };
    if (el.title) return { name: clip(trimText(el.title), MAX_NAME), from: 'title' };
    return { name: '', from: 'none' };
  }

  // textOf reads light DOM text plus every open shadow root, skipping descendants
  // that are hidden or that are themselves controls (an inner button is a separate
  // ref, not part of the outer name).
  function textOf(root) {
    var out = [];
    (function walk(node, insideHidden) {
      if (!node) return;
      if (node.nodeType === 3) {
        if (!insideHidden) out.push(node.nodeValue);
        return;
      }
      if (node.nodeType !== 1) return;
      var el = node;
      if (el !== root && (SKIP_TAGS[el.tagName] || insideHidden ||
          el.getAttribute('aria-hidden') === 'true' || el.hidden)) return;
      if (el !== root && isInteractive(el)) {
        // A nested control contributes its own text but is also listed on its
        // own; including it keeps names of composite widgets readable.
      }
      var kids = el.shadowRoot ? el.shadowRoot.childNodes : el.childNodes;
      for (var i = 0; i < kids.length; i++) walk(kids[i], insideHidden);
    })(root, false);
    return out.join(' ');
  }

  function labelFor(el, doc) {
    if (el.labels && el.labels.length) {
      var direct = trimText(textOf(el.labels[0]));
      if (direct) return direct;
    }
    if (el.id) {
      var holder = findInScope(doc, el.id);
      var scope = holder && holder.getRootNode ? holder.getRootNode() : doc;
      var viaFor = scope.querySelectorAll ? scope.querySelectorAll('label[for="' + cssEscape(el.id) + '"]') : [];
      if (viaFor && viaFor.length) {
        var t = trimText(textOf(viaFor[0]));
        if (t) return t;
      }
    }
    var wrapping = el.closest && el.closest('label');
    if (wrapping) {
      var w = trimText(textOf(wrapping));
      if (w) return w;
    }
    return '';
  }

  // ids live inside a shadow root as often as in the document, so a lookup has to
  // start from the element's own root.
  function findInScope(doc, id) {
    if (!id) return null;
    try {
      var byId = doc.getElementById ? doc.getElementById(id) : null;
      if (byId) return byId;
    } catch (e) { /* cross-document */ }
    return null;
  }

  function cssEscape(v) {
    if (window.CSS && CSS.escape) return CSS.escape(v);
    return String(v).replace(/[^a-zA-Z0-9_-]/g, function (c) { return '\\' + c; });
  }

  function stateOf(el) {
    var s = {};
    if (el.disabled) s.disabled = true;
    if (el.tagName === 'INPUT' && /^(checkbox|radio)$/.test(lower(el.getAttribute('type')) || '')) {
      s.checked = !!el.checked;
    }
    var ariaChecked = lower(el.getAttribute('aria-checked'));
    if (ariaChecked) s.checked = ariaChecked === 'true' || ariaChecked === 'mixed' ? ariaChecked : false;
    var expanded = lower(el.getAttribute('aria-expanded'));
    if (expanded) s.expanded = expanded === 'true';
    var selected = lower(el.getAttribute('aria-selected'));
    if (selected) s.selected = selected === 'true';
    if (el.tagName === 'OPTION') s.selected = !!el.selected;
    if (el.required) s.required = true;
    if (el.readOnly) s.readonly = true;
    if (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA') {
      if (el.autocomplete) s.autocomplete = trimText(el.autocomplete);
      if (el.type) s.type = lower(el.type);
    }
    if (/^H[1-6]$/.test(el.tagName)) s.level = Number(el.tagName.charAt(1));
    if (el.tagName === 'A' && el.href) s.href = el.href;
    var described = el.getAttribute('aria-describedby');
    if (described) {
      var parts = [];
      var ids = described.split(/\s+/);
      for (var i = 0; i < ids.length; i++) {
        var t = findInScope(document, ids[i]);
        if (t) parts.push(trimText(textOf(t)));
      }
      if (parts.length) s.description = clip(parts.join(' '), MAX_HINT);
    }
    return s;
  }

  function snapshot() {
    var out = {
      url: location.href,
      title: document.title || '',
      viewportH: window.innerHeight || 0,
      viewportW: window.innerWidth || 0,
      docH: Math.max(document.documentElement ? document.documentElement.scrollHeight : 0,
                     document.body ? document.body.scrollHeight : 0),
      scrollY: Math.round(window.scrollY || 0),
      elements: [],
      visited: [],
      frames: [],
      challenge: challengeEvidence(),
      truncated: false,
      skipped: 0
    };

    var seen = [];              // dedupe by role+name+box
    // Two maps live on window for the action layer:
    //   __argus_visited__ — traversal index → node, the stable identity
    //   __argus_refs__    — ref number  → node, what the agent's [n] resolves to
    // The Go side prunes and renumbers after us, and calls __argus_reassign__ to
    // make the ref map match the tree it printed. Without that step a ref points
    // at a different element than the row the agent read.
    var visitedMap = new Map();
    try {
      window.__argus_visited__ = visitedMap;
      window.__argus_reassign__ = function (indices) {
        var map = new Map();
        var byIndex = window.__argus_visited__ || new Map();
        // Clear through the visited map, not a selector: querySelectorAll cannot
        // reach inside a shadow root, and a stale ref there would silently point
        // at the wrong node.
        byIndex.forEach(function (node) {
          try { node.removeAttribute('data-af-ref'); } catch (e) {}
        });
        for (var j = 0; j < indices.length; j++) {
          var node = byIndex.get(indices[j]);
          if (!node) continue;
          var ref = map.size + 1;
          node.setAttribute('data-af-ref', String(ref));
          map.set(ref, node);
        }
        window.__argus_refs__ = map;
        return map.size;
      };
      window.__argus_refs__ = new Map();
    } catch (e) { /* sealed realm */ }
    // Every element the walk reaches, in the order Chrome's own accessibility
    // tree reaches it. The Go side zips this against Accessibility.getFullAXTree,
    // which needs no join key and no round trip per element.
    var visitIndex = 0;

    function push(el, info) {
      if (out.elements.length >= MAX_CANDIDATES) { out.truncated = true; return; }
      var box;
      try { box = el.getBoundingClientRect(); } catch (e) { return; }
      var key = info.role + '|' + info.name + '|' + Math.round(box.left) + ',' + Math.round(box.top) +
                ',' + Math.round(box.width) + ',' + Math.round(box.height);
      for (var i = 0; i < seen.length; i++) if (seen[i] === key) return;
      seen.push(key);

      var ref = out.elements.length + 1;
      el.setAttribute('data-af-ref', String(ref));
      el.setAttribute('data-af-idx', String(info.index));
      // window.__argus_refs__ is a Map from ref to node. An action resolves
      // through it rather than re-querying [data-af-ref], because a node the
      // framework unmounted should read as "gone" instead of as the new node that
      // happened to reuse the attribute.
      try {
        if (!window.__argus_refs__ || !(window.__argus_refs__ instanceof Map)) {
          window.__argus_refs__ = new Map();
        }
        window.__argus_refs__.set(ref, el);
      } catch (e) { /* sealed realm: the attribute is the fallback */ }

      out.elements.push({
        ref: ref,
        index: info.index,
        tag: lower(el.tagName),
        role: info.role,
        name: info.name,
        nameFrom: info.from,
        roleGuessed: !!info.guessed,
        x: Math.round(box.left),
        y: Math.round(box.top),
        w: Math.round(box.width),
        h: Math.round(box.height),
        interactive: !!info.interactive,
        structural: !!STRUCTURAL[info.role],
        belowFold: box.top > (window.innerHeight || 0),
        frame: info.frame || '',
        state: info.state
      });
    }

    function walk(node, depth, frameLabel, insideControl) {
      if (!node) return;
      if (node.nodeType !== 1) return;
      var el = node;
      if (SKIP_TAGS[el.tagName]) return;
      var doc = el.ownerDocument || document;

      // Indexed before any pruning decision: the accessibility tree Chrome
      // computes covers nodes we deliberately drop, and the zip only lines up if
      // both walks number the same elements in the same order.
      var index = visitIndex++;
      try {
        el.setAttribute('data-af-idx', String(index));
        out.visited.push([index, lower(el.tagName)]);
        visitedMap.set(index, el);
      } catch (e) { /* detached or sealed node: positional merge degrades, ref survives */ }

      if (!isVisible(el, doc)) return;

      var role = roleOf(el);
      if (role.role === 'none') return;
      var interactive = isInteractive(el);
      var name = nameOf(el, doc);

      // A container is not its contents. <main> whose text happens to include the
      // word "Buy" is not a buy button, and printing its whole subtree as a name
      // costs a page's worth of tokens. Only an explicit label names a landmark;
      // headings, cells and options keep their text, because for those the text IS
      // the content the agent asked for.
      if (!interactive && name.from === 'contents' && CONTAINER_ROLES[role.role]) {
        name = { name: '', from: 'none' };
      }

      // A control's text is its name; its children are not separate targets
      // unless they are themselves interactive (a link inside a button is).
      var keepAsTarget = interactive ||
        (role.role !== 'generic' && (STRUCTURAL[role.role] || depth <= 2) && name.name);

      if (keepAsTarget && !insideControl) {
        push(el, {
          role: role.role,
          name: name.name,
          from: name.from,
          guessed: role.guessed,
          interactive: interactive,
          state: stateOf(el),
          frame: frameLabel,
          index: index
        });
      } else if (keepAsTarget && insideControl && !interactive) {
        out.skipped++;
      }

      var nextInside = insideControl || interactive;
      var kids = el.shadowRoot ? el.shadowRoot.childNodes : el.childNodes;
      for (var i = 0; i < kids.length; i++) {
        var kid = kids[i];
        if (kid.nodeType === 1) {
          walk(kid, depth + 1, frameLabel, nextInside);
        }
      }

      // Same-origin iframes: their contents are part of this page as far as the
      // agent is concerned. Coordinates stay viewport-relative, which is what a
      // real mouse event needs, so no offsetting is required.
      if (el.tagName === 'IFRAME' || el.tagName === 'FRAME') {
        try {
          var childDoc = el.contentDocument;
          if (childDoc && childDoc.body) {
            var name = trimText(el.title || el.name || '') || hostOf(el.src || '') || 'frame';
            var label = frameLabel ? frameLabel + '/' + name : name;
            if (out.frames.indexOf(label) < 0) out.frames.push(label);
            walk(childDoc.body, 0, label, false);
          }
        } catch (e) {
          // Cross-origin: unreachable from this realm. The Go side can still list
          // it as a frame the agent may need to select.
          var blocked = hostOf(el.src || '') || '(cross-origin frame)';
          if (el.src && out.frames.indexOf(blocked) < 0) out.frames.push(blocked + ' (cross-origin)');
        }
      }
    }

    function hostOf(u) {
      try { return u ? new URL(u, location.href).host : ''; } catch (e) { return ''; }
    }

    var root = document.body || document.documentElement;
    if (root) walk(root, 0, '', false);

    out.count = out.elements.length;
    return JSON.stringify(out);
  }

  return snapshot();
})();
