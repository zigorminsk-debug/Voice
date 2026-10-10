// DocDiff — клиент веб-интерфейса.
"use strict";

const state = {
  fileA: null,
  fileB: null,
  filter: "all",
  mode: "positional",
  keyCol: "",
};

const $ = (id) => document.getElementById(id);

// ---------------------------------------------------------------------------
// Выбор файлов
// ---------------------------------------------------------------------------

function setupDropzone(side) {
  const drop = $("drop-" + side);
  const input = $("input-" + side);

  drop.addEventListener("click", () => input.click());
  drop.addEventListener("keydown", (e) => {
    if (e.key === "Enter" || e.key === " ") input.click();
  });

  input.addEventListener("change", () => {
    if (input.files.length) setFile(side, input.files[0]);
    input.value = "";
  });

  ["dragenter", "dragover"].forEach((ev) =>
    drop.addEventListener(ev, (e) => {
      e.preventDefault();
      drop.classList.add("drag");
    })
  );
  ["dragleave", "drop"].forEach((ev) =>
    drop.addEventListener(ev, (e) => {
      e.preventDefault();
      drop.classList.remove("drag");
    })
  );
  drop.addEventListener("drop", (e) => {
    if (e.dataTransfer.files.length) setFile(side, e.dataTransfer.files[0]);
  });

  $("clear-" + side).addEventListener("click", () => clearFile(side));
}

function fileExt(name) {
  const i = name.lastIndexOf(".");
  return i >= 0 ? name.slice(i).toLowerCase() : "";
}

function setFile(side, file) {
  const ext = fileExt(file.name);
  if (![".xlsx", ".xlsm", ".docx"].includes(ext)) {
    showError("Формат " + (ext || "без расширения") + " не поддерживается. Нужен .xlsx, .xlsm или .docx");
    return;
  }
  hideError();
  state["file" + side.toUpperCase()] = file;
  $("drop-" + side).hidden = true;
  $("info-" + side).hidden = false;
  $("name-" + side).textContent = file.name;
  $("size-" + side).textContent = formatSize(file.size);
  const t = $("type-" + side);
  t.textContent = ext === ".docx" ? "WORD" : "EXCEL";
  t.className = "file-type " + (ext === ".docx" ? "word" : "excel");
  updateCompareButton();
}

function clearFile(side) {
  state["file" + side.toUpperCase()] = null;
  $("drop-" + side).hidden = false;
  $("info-" + side).hidden = true;
  updateCompareButton();
}

function updateCompareButton() {
  const btn = $("btn-compare");
  const ok = state.fileA && state.fileB;
  btn.disabled = !ok;
  if (ok) {
    const extA = fileExt(state.fileA.name);
    const extB = fileExt(state.fileB.name);
    btn.classList.toggle("warn", extA !== extB);
    const isExcel = [".xlsx", ".xlsm"].includes(extA) && [".xlsx", ".xlsm"].includes(extB);
    $("options").hidden = !isExcel;
  } else {
    $("options").hidden = true;
  }
}

function setupOptions() {
  const mode = $("compare-mode");
  const key = $("key-col");
  const hint = $("key-hint");
  mode.addEventListener("change", () => {
    state.mode = mode.value;
    const isKey = mode.value === "key";
    key.hidden = !isKey;
    hint.hidden = !isKey;
  });
  key.addEventListener("input", () => {
    key.value = key.value.toUpperCase().replace(/[^A-Z]/g, "");
    state.keyCol = key.value;
  });
}

function formatSize(n) {
  if (n < 1024) return n + " Б";
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + " КБ";
  return (n / 1024 / 1024).toFixed(1) + " МБ";
}

// ---------------------------------------------------------------------------
// Сравнение
// ---------------------------------------------------------------------------

async function compare() {
  if (!state.fileA || !state.fileB) return;

  const extA = fileExt(state.fileA.name);
  const extB = fileExt(state.fileB.name);
  if (extA !== extB) {
    showError("Типы файлов не совпадают: " + extA + " и " + extB);
    return;
  }

  hideError();
  $("results").hidden = true;
  $("spinner").hidden = false;
  $("btn-compare").disabled = true;

  try {
    const form = new FormData();
    form.append("fileA", state.fileA);
    form.append("fileB", state.fileB);
    if (state.mode === "key") {
      form.append("keyCol", state.keyCol || "A");
    }
    const resp = await fetch("/api/compare", { method: "POST", body: form });
    const data = await resp.json();
    if (!resp.ok) throw new Error(data.error || "Ошибка сервера");
    renderResults(data);
  } catch (err) {
    showError(err.message);
  } finally {
    $("spinner").hidden = true;
    updateCompareButton();
  }
}

// ---------------------------------------------------------------------------
// Отрисовка результатов
// ---------------------------------------------------------------------------

function renderResults(data) {
  $("results").hidden = false;
  $("btn-download").href = "/api/download?id=" + encodeURIComponent(data.id);
  $("btn-download").setAttribute("download", data.name || "marked");
  $("btn-report").href = "/api/report?id=" + encodeURIComponent(data.id);

  const s = data.diff.summary;
  $("summary").innerHTML =
    card("modified", "Изменено", s.modified) +
    card("added", "Добавлено", s.added) +
    card("deleted", "Удалено", s.deleted);

  const box = $("changes");
  box.innerHTML = "";
  if (data.type === "excel") {
    renderExcel(box, data.diff);
  } else {
    renderWord(box, data.diff);
  }
  applyFilter();
  $("results").scrollIntoView({ behavior: "smooth", block: "start" });
}

function card(kind, label, value) {
  return (
    '<div class="sum-card ' + kind + '"><div class="sum-value">' + value +
    '</div><div class="sum-label">' + label + "</div></div>"
  );
}

function renderExcel(box, d) {
  if (!d.sheets || d.sheets.length === 0) {
    box.innerHTML = emptyBlock();
    return;
  }
  for (const sheet of d.sheets) {
    const sec = document.createElement("div");
    sec.className = "sheet-section";

    const h = document.createElement("h3");
    h.className = "sheet-title";
    const statusText = {
      same: "без изменений",
      changed: (sheet.changes || []).length + " изменений",
      onlyA: "только в оригинале",
      onlyB: "только в новом файле",
    }[sheet.status];
    h.innerHTML =
      '<span class="sheet-icon">' + (sheet.status === "changed" ? "📋" : "📁") +
      "</span> " + esc(sheet.name) + ' <span class="sheet-status">' + statusText + "</span>";
    sec.appendChild(h);

    if (sheet.status === "same" || !sheet.changes || sheet.changes.length === 0) {
      if (sheet.status === "onlyA" || sheet.status === "onlyB") {
        sec.appendChild(noteBlock(
          sheet.status === "onlyA"
            ? "Лист присутствует только в оригинале и отсутствует в новом файле."
            : "Лист присутствует только в новом файле."
        ));
      }
      box.appendChild(sec);
      continue;
    }

    const tbl = document.createElement("table");
    tbl.className = "diff-table";
    tbl.innerHTML =
      "<thead><tr><th>Тип</th><th>Строка</th><th>Изменения</th></tr></thead>";
    const tb = document.createElement("tbody");
    for (const ch of sheet.changes) {
      const tr = document.createElement("tr");
      tr.className = "change-row " + ch.type;
      tr.dataset.type = ch.type;

      let pos = "";
      if (ch.type === "deleted") pos = "A:" + ch.rowA;
      else if (ch.type === "added") pos = "B:" + ch.rowB;
      else pos = ch.rowA + " → " + ch.rowB;

      let detail = "";
      if (ch.type === "modified" && ch.cells) {
        detail = rowContext(ch) + ch.cells
          .map((c) => {
            let html = '<div class="cell-diff"><span class="col">' + esc(c.col) + "</span> ";
            if (c.formulaOnly) {
              html += '<span class="formula">формула: <code>=' + esc(c.oldFormula) + "</code> " +
                '<span class="arrow">→</span> <code>=' + esc(c.newFormula) + "</code></span> " +
                '<span class="muted-note">(значение не изменилось)</span>';
              return html + "</div>";
            }
            if (c.old) html += '<span class="old">' + esc(c.old) + "</span> ";
            html += '<span class="arrow">→</span> <span class="new">' + esc(c.new) + "</span>";
            if (c.oldFormula || c.newFormula) {
              if (c.oldFormula !== c.newFormula) {
                html += ' <span class="formula">формула: <code>=' + esc(c.oldFormula) +
                  '</code> <span class="arrow">→</span> <code>=' + esc(c.newFormula) + "</code></span>";
              } else {
                html += ' <span class="formula muted-note">формула: <code>=' + esc(c.newFormula) + "</code></span>";
              }
            }
            return html + "</div>";
          })
          .join("");
      } else {
        const vals = ch.type === "deleted" ? ch.old : ch.new;
        detail = renderValues(vals);
      }

      tr.innerHTML =
        "<td>" + badge(ch.type) + "</td>" +
        '<td class="rownum">' + pos + "</td>" +
        "<td>" + detail + "</td>";
      tb.appendChild(tr);
    }
    tbl.appendChild(tb);
    sec.appendChild(tbl);
    box.appendChild(sec);
  }
}

function rowContext(ch) {
  const changed = new Set((ch.cells || []).map((c) => c.col));
  const src = ch.new || ch.old || {};
  const parts = Object.keys(src)
    .sort((a, b) => colIndex(a) - colIndex(b))
    .filter((k) => !changed.has(k) && src[k] !== "")
    .map((k) => '<span class="col">' + esc(k) + "</span>: " + esc(src[k]));
  return parts.length ? '<div class="row-context">' + parts.join(" · ") + "</div>" : "";
}

function renderValues(map) {
  if (!map) return "";
  const keys = Object.keys(map).sort((a, b) => colIndex(a) - colIndex(b));
  return keys
    .map((k) => '<span class="kv"><span class="col">' + esc(k) + "</span>: " + esc(map[k]) + "</span>")
    .join(" ");
}

function colIndex(col) {
  let n = 0;
  for (const ch of col.toUpperCase()) n = n * 26 + (ch.charCodeAt(0) - 64);
  return n;
}

function renderWord(box, d) {
  if (!d.blocks || d.blocks.length === 0) {
    box.innerHTML = emptyBlock();
    return;
  }
  const list = document.createElement("div");
  list.className = "block-list";

  for (const b of d.blocks) {
    const item = document.createElement("div");
    item.className = "change-row block " + b.type;
    item.dataset.type = b.type;

    let body = "";
    if (b.type === "modified" && b.segments) {
      body = b.segments
        .map((s) => {
          if (s.type === "eq") return esc(s.text);
          if (s.type === "ins") return '<ins class="seg-ins">' + esc(s.text) + "</ins>";
          return '<del class="seg-del">' + esc(s.text) + "</del>";
        })
        .join("");
    } else if (b.type === "added") {
      body = '<ins class="seg-ins">' + esc(b.new) + "</ins>";
    } else {
      body = '<del class="seg-del">' + esc(b.old) + "</del>";
    }

    const kind = b.kind === "table" ? "Таблица" : "Абзац";
    const rowsNote =
      b.rows && b.rows.length
        ? '<div class="table-changes">Изменения в таблице: ' +
          b.rows
            .map((r) => {
              const cells = (r.cells || [])
                .map((c) => esc(c.col) + ": " + esc(c.old) + " → " + esc(c.new))
                .join("; ");
              return badge(r.type) + " строка " + (r.rowB || r.rowA) + (cells ? " — " + cells : "");
            })
            .join(" · ") +
          "</div>"
        : "";

    item.innerHTML =
      '<div class="block-head">' + badge(b.type) + ' <span class="block-kind">' + kind + "</span></div>" +
      '<div class="block-text">' + body + "</div>" + rowsNote;
    list.appendChild(item);
  }
  box.appendChild(list);
}

function badge(type) {
  const labels = { modified: "Изменено", added: "Добавлено", deleted: "Удалено" };
  return '<span class="badge ' + type + '">' + labels[type] + "</span>";
}

function noteBlock(text) {
  const d = document.createElement("div");
  d.className = "note-block";
  d.textContent = text;
  return d;
}

function emptyBlock() {
  return '<div class="note-block">Изменений не найдено — файлы совпадают.</div>';
}

function esc(s) {
  return String(s == null ? "" : s)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

// ---------------------------------------------------------------------------
// Фильтры и ошибки
// ---------------------------------------------------------------------------

function applyFilter() {
  document.querySelectorAll(".change-row").forEach((el) => {
    el.hidden = state.filter !== "all" && el.dataset.type !== state.filter;
  });
  document.querySelectorAll(".sheet-section").forEach((sec) => {
    const visible = sec.querySelectorAll(".change-row:not([hidden])").length;
    const hasRows = sec.querySelectorAll(".change-row").length > 0;
    sec.hidden = hasRows && visible === 0;
  });
}

function showError(msg) {
  const box = $("error-box");
  box.textContent = msg;
  box.hidden = false;
}

function hideError() {
  $("error-box").hidden = true;
}

// ---------------------------------------------------------------------------
// Инициализация
// ---------------------------------------------------------------------------

setupDropzone("a");
setupDropzone("b");
setupOptions();
$("btn-compare").addEventListener("click", compare);
$("filters").addEventListener("click", (e) => {
  const btn = e.target.closest(".chip");
  if (!btn) return;
  state.filter = btn.dataset.filter;
  $("filters").querySelectorAll(".chip").forEach((c) => c.classList.remove("active"));
  btn.classList.add("active");
  applyFilter();
});
