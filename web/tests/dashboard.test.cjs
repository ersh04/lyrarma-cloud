const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const { test } = require("node:test");

const source = fs.readFileSync(path.join(__dirname, "../static/dashboard.js"), "utf8")
  .replace("  initialize();", "  globalThis.hooks = { updateStorageUsage, bindUploadForm };");
const translations = JSON.parse(fs.readFileSync(path.join(__dirname, "../static/translations.json"), "utf8"));

function setup(language = "en") {
  const elements = new Map();
  function element(id) {
    if (!elements.has(id)) {
      elements.set(id, {
        dataset: {}, style: {}, attributes: {}, handlers: {}, hidden: false,
        classList: { add() {}, remove() {} },
        addEventListener(event, handler) { this.handlers[event] = handler; },
        setAttribute(name, value) { this.attributes[name] = value; },
      });
    }
    return elements.get(id);
  }
  const root = element("dashboard");
  const t = translations[language];
  Object.assign(root.dataset, {
    maxUploadMb: "-1", unlimited: t.unlimited, sizeGbUnit: t.size_gb_unit,
    storageOf: t.storage_of, fileTooLargeDetail: t.file_too_large_detail,
    uploadProgress: t.upload_progress, uploadComplete: t.upload_complete,
    uploadRejectedNoLimit: t.upload_rejected_no_limit,
  });
  let uploads = 0;
  const context = {
    Intl, URL, document: {
      documentElement: { lang: language },
      querySelector: () => root,
      getElementById: element,
    },
    window: { location: { origin: "https://cloud.example.test" } },
    FormData: class { set() {} },
    XMLHttpRequest: class {
      constructor() { this.handlers = {}; this.upload = { addEventListener() {} }; }
      addEventListener(event, handler) { this.handlers[event] = handler; }
      open() {}
      setRequestHeader() {}
      send() { uploads++; this.status = 201; this.handlers.load(); }
    },
  };
  vm.createContext(context);
  vm.runInContext(source, context);
  return { context, element, root, uploads: () => uploads };
}

for (const language of ["ru", "en"]) {
  test(`storage refresh switches between finite and unlimited: ${language}`, () => {
    const { context, element, root } = setup(language);
    const gb = 1024 ** 3;
    const usage = { plan: ["user"], max_storage_size: -1, max_upload_size: -1, used_storage_size: 25 * gb, reserved_storage_size: gb };
    context.hooks.updateStorageUsage(usage);
    assert.equal(element("storageLimit").textContent, translations[language].unlimited);
    assert.equal(element("storageOf").textContent, "·");
    assert.equal(element("storageProgress").hidden, true);
    assert.equal(element("storageLimitUnit").hidden, true);
    assert.equal(element("storageReserved").hidden, false);
    assert.equal(element("storageUsed").textContent, language === "ru" ? "26,00" : "26.00");
    assert.equal(element("uploadLimit").textContent, translations[language].unlimited);
    assert.equal(element("uploadLimitUnit").hidden, true);
    assert.equal(root.dataset.maxUploadMb, "-1");

    context.hooks.updateStorageUsage({ ...usage, max_storage_size: 30 * gb, max_upload_size: gb });
    assert.equal(element("storageProgress").hidden, false);
    assert.equal(element("storageOf").textContent, translations[language].storage_of);
    assert.equal(element("storageLimitUnit").hidden, false);
    assert.equal(element("storageUsage").dataset.state, "warning");
    assert.equal(element("uploadLimitUnit").hidden, false);
    assert.equal(root.dataset.maxUploadMb, "1024");

    context.hooks.updateStorageUsage(usage);
    assert.equal(element("storageProgress").hidden, true);
    assert.equal(element("storageUsage").dataset.state, "normal");
    assert.equal(root.dataset.maxUploadMb, "-1");
  });
}

test("upload validation honors unlimited and refreshed finite limits", async () => {
  for (const limit of [-1, 1]) {
    const { context, element, root, uploads } = setup();
    element("fileInput").files = [{ name: "large.bin", size: 5 * 1024 ** 2 }];
    context.hooks.bindUploadForm();
    root.dataset.maxUploadMb = String(limit);
    await element("uploadForm").handlers.submit({ preventDefault() {} });
    assert.equal(uploads(), limit === -1 ? 1 : 0);
    if (limit === -1) {
      assert.match(context.window.location.href, /\/dashboard\?/);
    } else {
      assert.equal(element("progressBar").style.width, "0%");
      assert.ok(element("progressLabel").textContent.length > 0);
    }
  }
});
