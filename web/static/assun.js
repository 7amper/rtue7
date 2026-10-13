// ASSUN page: renders register tables grouped by Classify and handles live read/write.
// Read All  — reads via Modbus RTU all registers of the current tab (Classify),
//             size taken from Data Type (Uint16/Int16 = 1 reg, Uint32/Int32 = 2 regs).
// Read      — reads only the selected row's register.
// Write All — writes all editable cells of the selected Classify (current tab).
// Write     — writes only the selected row's cell.
(function () {
  'use strict';

  var TABS = [
    { name: 'basic',         classes: ['1'] },
    { name: 'current',       classes: ['2'] },
    { name: 'speed',         classes: ['3'] },
    { name: 'position',      classes: ['4'] },
    { name: 'communication', classes: ['5'] },
    { name: 'motor',         classes: ['6'] },
    { name: 'gain',          classes: ['7'] },
    { name: 'vibration',     classes: ['8'] },
    { name: 'dido',          classes: ['9'] },
    { name: 'readonly',      classes: ['12', '13'] }
  ];

  var params = [];        // rows from embedded JSON (assun.csv)
  var remote = null;      // rows fetched from /api/parameters?file=etalon.csv
  var liveValues = {};    // addr(HEX, no prefix) -> raw int64 from controller
  var activeTab = 'basic';

  function esc(s) {
    return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
                    .replace(/"/g, '&quot;');
  }

  // Notes contain <br> tags coming straight from the table; everything else is escaped.
  function notesHtml(s) {
    return esc(s).replace(/&lt;br\s*\/?&gt;/gi, '<br>');
  }

  function decPlaces(dec) {
    // F0..F4 -> количество знаков после запятой
    var n = parseInt((dec || '').replace(/[^0-9]/g, ''), 10);
    if (isNaN(n)) n = 0;
    return n;
  }

  function decimalFactor(dec) {
    return Math.pow(10, decPlaces(dec));
  }

  // Number of 16-bit Modbus registers occupied by the given Data Type.
  function regCount(dataType) {
    return /32/i.test(dataType || '') ? 2 : 1;
  }

  // Interpret raw register value according to Data Type column:
  // Uint16 — unsigned 16-bit, Int16 — signed 16-bit (two's complement),
  // Uint32 — unsigned 32-bit, Int32 — signed 32-bit (two's complement).
  function signed(raw, dataType) {
    var dt = String(dataType || '').toLowerCase();
    if (dt === 'int32') { return raw | 0; }                    // int32 two's complement
    if (dt === 'int16') { return (raw << 16) >> 16; }          // int16 two's complement
    if (dt === 'uint32') { return raw >>> 0; }                 // unsigned 32-bit
    /* uint16 (и прочие 16-битные) */
    return raw & 0xFFFF;                                       // unsigned 16-bit
  }

  function toText(raw, dataType, dec) {
    var v = signed(raw, dataType);
    var d = decPlaces(dec);
    if (d > 0) return (v / Math.pow(10, d)).toFixed(d);
    return String(Math.round(v));
  }

  // Displayed value for read-only cells: live controller value or CSV default.
  function displayValue(p) {
    var key = hexKey(p.address);
    if (!(key in liveValues)) return esc(p.value);
    return esc(toText(liveValues[key], p.dataType, p.decimal)) + ' <span class="live">(live)</span>';
  }

  // Value shown in an editable input after a Read All: live controller value
  // (formatted per Data Type / Decimal place) or the CSV default.
  function inputValue(p) {
    var key = hexKey(p.address);
    if (!(key in liveValues)) return esc(p.value);
    return esc(toText(liveValues[key], p.dataType, p.decimal));
  }

  function rowHtml(p) {
    var editable = !(p.classify === '12' || p.classify === '13');
    var cell = editable
      ? '<td class="val-cell"><input data-addr="' + esc(p.address) + '" data-type="' + esc(p.dataType) + '"' +
        ' value="' + inputValue(p) + '" title="Press Enter or click Write to save to the controller"></td>'
      : '<td class="val-cell readonly">' + displayValue(p) + '</td>';
    return '<tr>' +
      '<td class="addr">' + esc(p.address) + '</td>' +
      '<td>' + esc(p.name) + '</td>' + cell +
      '<td>' + esc(p.unit) + '</td>' +
      '<td>' + esc(p.dataType) + '</td>' +
      '<td class="notes">' + notesHtml(p.notes) + '</td>' +
      '<td class="row-actions">' +
        '<button class="btn-read" title="Read this register (' + regCount(p.dataType) +
          ') from the controller">Read</button>' +
        (editable
          ? '<button class="btn-write" title="Write this cell to the controller">Write</button>'
          : '') +
      '</td>' +
      '</tr>';
  }

  function tableHtml(list) {
    if (!list.length) return '<p class="placeholder">No parameters in this group.</p>';
    var h = '<table class="reg-table"><thead><tr>' +
      '<th>Address</th><th>Name</th><th>Value</th><th>Unit</th><th>Data Type</th><th>Notes</th>' +
      '<th>Actions</th>' +
      '</tr></thead><tbody>';
    for (var i = 0; i < list.length; i++) h += rowHtml(list[i]);
    return h + '</tbody></table>';
  }

  function currentSource() {
    var sel = document.getElementById('fileSel');
    return sel ? sel.value : 'assun.csv';
  }

  function currentData() {
    var src = currentSource();
    return (src === 'assun.csv') ? params : remote;
  }

  function tabClasses(name) {
    for (var i = 0; i < TABS.length; i++) {
      if (TABS[i].name === name) return TABS[i].classes;
    }
    return [];
  }

  // Rows of the currently selected Classify tab.
  function currentTabRows() {
    var data = currentData();
    if (!data) return [];
    var classes = tabClasses(activeTab);
    return data.filter(function (p) { return classes.indexOf(p.classify) >= 0; });
  }

  // ---- second-level tab switching (called from inline onclick in assun.html) ----
  function showTab(name) {
    activeTab = name;
    document.querySelectorAll('.subtab-btn').forEach(function (b) {
      b.classList.toggle('active', b.dataset.tab === name);
    });
    document.querySelectorAll('.tab-content').forEach(function (el) {
      el.classList.toggle('active', el.id === 'tab-' + name);
    });
  }
  window.showTab = showTab;

  function render() {
    var src = currentSource();
    var data = (src === 'assun.csv') ? params : remote;
    if (!data) { setStatus('Loading etalon.csv...'); return; }
    TABS.forEach(function (t) {
      var el = document.getElementById('tab-' + t.name);
      if (!el) return;
      var list = data.filter(function (p) { return t.classes.indexOf(p.classify) >= 0; });
      el.innerHTML = tableHtml(list);
    });
    bindRowHandlers();
  }

  function setStatus(msg) {
    var el = document.getElementById('statusMsg');
    if (el) el.textContent = msg;
  }

  function hexKey(addr) { return String(addr).toLowerCase().replace(/^0x/, '').toUpperCase(); }

  function getSlave() {
    return parseInt(document.getElementById('slaveId').value, 10) || 1;
  }

  // Update one row after a successful read: live values map + input field.
  function applyLiveValue(p, raw) {
    liveValues[hexKey(p.address)] = raw;
    var valCell = findValInput(p.address);
    if (valCell) valCell.value = toText(raw, p.dataType, p.decimal);
    else render(); // read-only cells are not inputs — re-render text
  }

  function findValInput(address) {
    var norm = hexKey(address);
    var inp = document.querySelector('input[data-addr]');
    var found = null;
    document.querySelectorAll('input[data-addr]').forEach(function (el) {
      if (hexKey(el.dataset.addr) === norm) found = el;
    });
    return found;
  }

  // Parse user-entered value into raw integer pattern according to Data Type.
  function parseRaw(valueStr, dataType, dec) {
    var s = String(valueStr).trim();
    var raw;
    if (/^0x/i.test(s)) {
      raw = parseInt(s, 16);
    } else {
      var num = parseFloat(s);
      if (isNaN(num)) return null;
      raw = Math.round(num * decimalFactor(dec));
    }
    if (isNaN(raw)) return null;
    var dt = String(dataType || '').toLowerCase();
    if (dt === 'uint32') {
      return raw >= 0 ? raw : raw + 0x100000000; // беззнаковый шаблон 32 бита
    }
    if (dt === 'int32') {
      if (raw < -0x80000000 || raw > 0x7FFFFFFF) return null;
      return raw;                                // сервер приведёт к дополнительному коду
    }
    if (dt === 'int16') {
      if (raw < -0x8000 || raw > 0x7FFF) return null;
      return raw;
    }
    /* uint16 */
    if (raw < 0 || raw > 0xFFFF) return null;
    return raw;
  }

  // ---- single row read ----
  function readRow(tr) {
    var p = rowParams[tr.dataset.rowIdx];
    var slave = getSlave();
    setStatus('Reading ' + p.address + ' (' + p.dataType + ') from slave ' + slave + '...');
    fetch('/api/read?address=' + encodeURIComponent(p.address) +
          '&type=' + encodeURIComponent(p.dataType) + '&slave=' + slave)
      .then(function (r) { return r.json(); })
      .then(function (j) {
        if (j.error) { setStatus('Read failed ' + p.address + ': ' + j.error); return; }
        applyLiveValue(p, j.value);
        setStatus('Read OK: ' + p.address + ' = ' + toText(j.value, p.dataType, p.decimal));
      })
      .catch(function (err) { setStatus('Read error: ' + err); });
  }

  // ---- single row write ----
  function writeRow(tr) {
    var p = rowParams[tr.dataset.rowIdx];
    var inp = findValInput(p.address);
    var valueStr = inp ? inp.value : p.value;
    writeOne(p, valueStr, function (ok, err) {
      if (ok) setStatus('Written ' + p.address + ' = ' + valueStr);
      else setStatus('Write failed ' + p.address + ': ' + err);
    });
  }

  function writeOne(p, valueStr, cb) {
    var raw = parseRaw(valueStr, p.dataType, p.decimal);
    if (raw === null) { cb(false, 'invalid value "' + valueStr + '"'); return; }
    var body = {
      slave: getSlave(),
      address: p.address,
      value: String(raw),   // already scaled by decimal places
      dataType: p.dataType
    };
    fetch('/api/write', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    }).then(function (r) { return r.json(); })
      .then(function (j) { cb(!!j.ok, j.error || 'unknown'); })
      .catch(function (err) { cb(false, String(err)); });
  }

  // ---- Read All: read every register of the current Classify tab ----
  function readAll() {
    var rows = currentTabRows();
    if (!rows.length) { setStatus('Nothing to read'); return; }
    var slave = getSlave();
    var addrs = rows.map(function (p) {
      // адрес + Data Type: сервер читает 1 или 2 регистра и интерпретирует
      // значение согласно типу (Uint16/Int16/Uint32/Int32)
      return p.address.replace(/^0x/i, '') + '@' + (p.dataType || 'Uint16');
    });
    setStatus('Reading ' + addrs.length + ' registers of tab "' + activeTab + '" from slave ' + slave + '...');
    fetch('/api/parameters?file=' + encodeURIComponent(currentSource()) +
          '&slave=' + slave + '&regs=' + addrs.join(','))
      .then(function (r) { return r.json(); })
      .then(function (j) {
        var vals = j.values || {};
        var n = Object.keys(vals).length;
        // Сохраняем введённые пользователем значения полей, если их строка
        // не была прочитана (например, обрыв групповой операции чтения).
        var typedVals = collectTypedValues();
        Object.keys(vals).forEach(function (k) { liveValues[k.toUpperCase()] = vals[k]; });
        render();
        Object.keys(typedVals).forEach(function (k) {
          if (!(k in vals)) {
            var inp = findValInput(k);
            if (inp) inp.value = typedVals[k];
          }
        });
        if (j.error && !n) { setStatus('Read error: ' + j.error); return; }
        setStatus('Read All OK (' + n + ' values)' + (j.error ? ' — partial: ' + j.error : ''));
      })
      .catch(function (err) { setStatus('Read error: ' + err); });
  }

  // Snapshot of current editable input values keyed by HEX address.
  function collectTypedValues() {
    var out = {};
    document.querySelectorAll('input[data-addr]').forEach(function (el) {
      out[hexKey(el.dataset.addr)] = el.value;
    });
    return out;
  }

  // ---- Write All: write every editable cell of the current Classify tab ----
  function writeAll() {
    var rows = currentTabRows().filter(function (p) {
      return !(p.classify === '12' || p.classify === '13');
    });
    if (!rows.length) { setStatus('Nothing to write in this Classify'); return; }
    var btn = document.getElementById('btnWriteAll');
    btn.disabled = true;
    setStatus('Writing ' + rows.length + ' cells of tab "' + activeTab + '"...');
    var done = 0, failed = 0;
    var byAddr = {};
    rows.forEach(function (p) { byAddr[hexKey(p.address)] = p; });
    function next(i) {
      if (i >= rows.length) {
        btn.disabled = false;
        setStatus('Write All done: ' + (done - failed) + ' ok, ' + failed + ' failed');
        // После успешной записи перечитываем регистры вкладки, чтобы колонка
        // Value показала фактическое содержимое контроллера.
        readAll();
        return;
      }
      var p = rows[i];
      var inp = findValInput(p.address);
      var valueStr = inp ? inp.value : p.value;
      writeOne(p, valueStr, function (ok, err) {
        done++;
        if (!ok) {
          failed++;
          console.warn('Write failed ' + p.address + ': ' + err);
        }
        next(i + 1);
      });
    }
    next(0);
  }

  // ---- row action buttons + Enter-to-write ----
  var rowParams = [];

  function bindRowHandlers() {
    rowParams = [];
    var data = currentData() || [];
    document.querySelectorAll('.reg-table tbody tr').forEach(function (tr) {
      var addrTd = tr.querySelector('td.addr');
      if (!addrTd) return;
      var p = data.find(function (x) { return hexKey(x.address) === hexKey(addrTd.textContent); });
      if (!p) return;
      tr.dataset.rowIdx = rowParams.length;
      rowParams.push(p);
      var br = tr.querySelector('.btn-read');
      if (br) br.addEventListener('click', function () { readRow(tr); });
      var bw = tr.querySelector('.btn-write');
      if (bw) bw.addEventListener('click', function () { writeRow(tr); });
    });
    document.querySelectorAll('input[data-addr]').forEach(function (inp) {
      inp.addEventListener('keydown', function (e) {
        if (e.key === 'Enter') {
          e.preventDefault();
          var tr = inp.closest('tr');
          if (tr) writeRow(tr);
        }
      });
    });
  }

  function loadRemoteIfNeeded() {
    if (remote) { render(); return; }
    fetch('/api/parameters?file=etalon.csv')
      .then(function (r) { return r.json(); })
      .then(function (j) {
        remote = j.params || [];
        render();
        setStatus('Etalon loaded (' + remote.length + ' rows)');
      })
      .catch(function () { remote = []; render(); setStatus('Etalon file not found'); });
  }

  // ---- init ----
  try {
    params = JSON.parse(document.getElementById('assunData').textContent);
  } catch (e) { params = []; }

  document.getElementById('btnReadAll').addEventListener('click', readAll);
  document.getElementById('btnWriteAll').addEventListener('click', writeAll);
  document.getElementById('btnReload').addEventListener('click', function () {
    liveValues = {};
    if (currentSource() === 'etalon.csv') loadRemoteIfNeeded(); else render();
    setStatus('Table reloaded from CSV');
  });
  document.getElementById('fileSel').addEventListener('change', function () {
    liveValues = {};
    if (currentSource() === 'etalon.csv') loadRemoteIfNeeded(); else render();
  });

  render();
})();
