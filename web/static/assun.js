// ASSUN page: renders register tables grouped by Classify and handles live read/write.
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

  function esc(s) {
    return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
                    .replace(/"/g, '&quot;');
  }

  // Notes contain <br> tags coming straight from the table; everything else is escaped.
  function notesHtml(s) {
    return esc(s).replace(/&lt;br\s*\/?&gt;/gi, '<br>');
  }

  function decimalFactor(dec) {
    // F0..F4 -> 10^dec
    var n = parseInt((dec || '').replace(/[^0-9]/g, ''), 10);
    if (isNaN(n)) n = 0;
    return Math.pow(10, n);
  }

  function signed(raw, dataType) {
    if (/int32/i.test(dataType)) { raw |= 0; }
    else if (/int16/i.test(dataType)) { raw = (raw << 16) >> 16; }
    return raw;
  }

  function displayValue(p) {
    var key = p.address.toLowerCase().replace(/^0x/, '');
    if (!(key in liveValues)) return esc(p.value);
    var raw = signed(liveValues[key] & 0xFFFFFFFF, p.dataType);
    var f = decimalFactor(p.decimal);
    var v = raw / f;
    var txt = (f > 1) ? v.toFixed(Math.round(Math.log10(f))) : String(Math.round(v));
    return esc(txt) + ' <span class="live">(live)</span>';
  }

  function rowHtml(p) {
    var ro = p.displayMode === 'H' && TABS.some(function (t) { return t.classes.indexOf(p.classify) >= 0 && (p.classify === '12' || p.classify === '13'); });
    var editable = !(p.classify === '12' || p.classify === '13');
    var cell = editable
      ? '<td class="val-cell"><input data-addr="' + esc(p.address) + '" data-type="' + esc(p.dataType) + '"' +
        ' value="' + esc(p.value) + '" title="Click Read All to refresh; press Enter to write"></td>'
      : '<td class="val-cell readonly">' + displayValue(p) + '</td>';
    return '<tr>' +
      '<td class="addr">' + esc(p.address) + '</td>' +
      '<td>' + esc(p.name) + '</td>' + cell +
      '<td>' + esc(p.unit) + '</td>' +
      '<td>' + esc(p.dataType) + '</td>' +
      '<td class="notes">' + notesHtml(p.notes) + '</td>' +
      '</tr>';
  }

  function tableHtml(list) {
    if (!list.length) return '<p class="placeholder">No parameters in this group.</p>';
    var h = '<table class="reg-table"><thead><tr>' +
      '<th>Address</th><th>Name</th><th>Value</th><th>Unit</th><th>Data Type</th><th>Notes</th>' +
      '</tr></thead><tbody>';
    for (var i = 0; i < list.length; i++) h += rowHtml(list[i]);
    return h + '</tbody></table>';
  }

  function currentSource() {
    var sel = document.getElementById('fileSel');
    return sel ? sel.value : 'assun.csv';
  }

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
    bindWriteHandlers();
  }

  function setStatus(msg) {
    var el = document.getElementById('statusMsg');
    if (el) el.textContent = msg;
  }

  function hexKey(addr) { return addr.toLowerCase().replace(/^0x/, '').toUpperCase(); }

  function bindWriteHandlers() {
    document.querySelectorAll('input[data-addr]').forEach(function (inp) {
      inp.addEventListener('keydown', function (e) {
        if (e.key === 'Enter') {
          e.preventDefault();
          writeParam(inp);
        }
      });
    });
  }

  function writeParam(inp) {
    var slave = parseInt(document.getElementById('slaveId').value, 10) || 1;
    var body = {
      slave: slave,
      address: inp.dataset.addr,
      value: inp.value,
      dataType: inp.dataset.type
    };
    setStatus('Writing ' + inp.dataset.addr + '...');
    fetch('/api/write', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    }).then(function (r) { return r.json(); })
      .then(function (j) {
        setStatus(j.ok ? ('Written ' + inp.dataset.addr) : ('Write failed: ' + (j.error || 'unknown')));
      })
      .catch(function (err) { setStatus('Write error: ' + err); });
  }

  function readAll() {
    var slave = parseInt(document.getElementById('slaveId').value, 10) || 1;
    var src = currentSource();
    var data = (src === 'assun.csv') ? params : remote;
    if (!data) { setStatus('Data not loaded'); return; }
    var addrs = data.map(function (p) { return p.address.replace(/^0x/i, ''); });
    setStatus('Reading ' + addrs.length + ' registers from slave ' + slave + '...');
    fetch('/api/parameters?file=' + encodeURIComponent(src) + '&slave=' + slave + '&regs=' + addrs.join(','))
      .then(function (r) { return r.json(); })
      .then(function (j) {
        if (j.error) { setStatus('Read error: ' + j.error); return; }
        liveValues = j.values || {};
        render();
        setStatus('Read OK (' + Object.keys(liveValues).length + ' values, live=true)');
      })
      .catch(function (err) { setStatus('Read error: ' + err); });
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
