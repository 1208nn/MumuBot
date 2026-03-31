import htmx from "htmx.org";
import Alpine from "alpinejs";
import "preline";

window.htmx = htmx;
window.Alpine = Alpine;

const DEFAULT_TOAST_DELAY = 4200;
const REDUCED_MOTION = window.matchMedia("(prefers-reduced-motion: reduce)");

const TOAST_CLASS_BY_KIND = {
  error:
    "pointer-events-auto w-full rounded-[1.45rem] border bg-white/96 p-4 text-slate-900 shadow-[0_24px_60px_rgba(8,17,31,0.16)] ring-1 backdrop-blur-xl sm:w-[24rem] border-rose-200/90 ring-rose-100/80",
  warn:
    "pointer-events-auto w-full rounded-[1.45rem] border bg-white/96 p-4 text-slate-900 shadow-[0_24px_60px_rgba(8,17,31,0.16)] ring-1 backdrop-blur-xl sm:w-[24rem] border-amber-200/90 ring-amber-100/80",
  success:
    "pointer-events-auto w-full rounded-[1.45rem] border bg-white/96 p-4 text-slate-900 shadow-[0_24px_60px_rgba(8,17,31,0.16)] ring-1 backdrop-blur-xl sm:w-[24rem] border-emerald-200/90 ring-emerald-100/80",
};

const TOAST_ICON_CLASS_BY_KIND = {
  error: "inline-flex size-10 shrink-0 items-center justify-center rounded-2xl ring-1 bg-rose-50 text-rose-700 ring-rose-200/80",
  warn: "inline-flex size-10 shrink-0 items-center justify-center rounded-2xl ring-1 bg-amber-50 text-amber-700 ring-amber-200/80",
  success: "inline-flex size-10 shrink-0 items-center justify-center rounded-2xl ring-1 bg-emerald-50 text-emerald-700 ring-emerald-200/80",
};

const TOAST_CLOSE_CLASS_BY_KIND = {
  error:
    "inline-flex size-9 shrink-0 items-center justify-center rounded-full text-slate-500 transition duration-200 ease-out hover:bg-slate-950/6 hover:text-slate-700 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:ring-offset-white focus-visible:ring-rose-300",
  warn:
    "inline-flex size-9 shrink-0 items-center justify-center rounded-full text-slate-500 transition duration-200 ease-out hover:bg-slate-950/6 hover:text-slate-700 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:ring-offset-white focus-visible:ring-amber-300",
  success:
    "inline-flex size-9 shrink-0 items-center justify-center rounded-full text-slate-500 transition duration-200 ease-out hover:bg-slate-950/6 hover:text-slate-700 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:ring-offset-white focus-visible:ring-emerald-300",
};

const ACTION_CHIP_CLASS_BY_KIND = {
  cyan: "inline-flex items-center rounded-full bg-cyan-50 px-3 py-1 text-[11px] font-semibold text-cyan-700 ring-1 ring-cyan-200/80",
  teal: "inline-flex items-center rounded-full bg-teal-50 px-3 py-1 text-[11px] font-semibold text-teal-700 ring-1 ring-teal-200/80",
  neutral: "inline-flex items-center rounded-full bg-slate-100 px-3 py-1 text-[11px] font-semibold text-slate-600 ring-1 ring-slate-200/80",
};

const DEFAULT_ACTION_SUBMIT_CLASS =
  "inline-flex items-center justify-center gap-2 rounded-2xl bg-[linear-gradient(135deg,#101a32_0%,#1e2c4f_58%,#0b7285_120%)] px-4 py-2.5 text-[13px] font-semibold text-white shadow-[0_18px_32px_rgba(15,23,42,0.16)] transition duration-200 ease-out hover:brightness-105 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-cyan-300 focus-visible:ring-offset-2 focus-visible:ring-offset-white disabled:cursor-wait disabled:opacity-70";

function initAdminInteractions() {
  if (window.HSStaticMethods && typeof window.HSStaticMethods.autoInit === "function") {
    window.HSStaticMethods.autoInit();
  }
}

function markFormSubmitting(form, activeButton) {
  if (!(form instanceof HTMLFormElement)) {
    return;
  }

  form.dataset.submitting = "true";
  const buttons = form.querySelectorAll('button[type="submit"]');
  buttons.forEach((button, index) => {
    button.dataset.submitIndex = String(index);
    button.disabled = true;
    button.classList.add("opacity-70", "cursor-wait");
  });

  if (activeButton instanceof HTMLButtonElement) {
    form.dataset.activeSubmitIndex = activeButton.dataset.submitIndex || "";
    const label = activeButton.getAttribute("data-loading-label");
    if (label) {
      activeButton.dataset.originalLabel = activeButton.textContent || "";
      activeButton.textContent = label;
    }
  }
}

function resetFormSubmitting(form) {
  if (!(form instanceof HTMLFormElement)) {
    return;
  }

  delete form.dataset.submitting;
  delete form.dataset.activeSubmitIndex;

  const buttons = form.querySelectorAll('button[type="submit"]');
  buttons.forEach((button) => {
    button.disabled = false;
    button.classList.remove("opacity-70", "cursor-wait");
    if (button.dataset.originalLabel) {
      button.textContent = button.dataset.originalLabel;
      delete button.dataset.originalLabel;
    }
  });
}

function formFromEvent(event) {
  const elt = event && event.detail ? event.detail.elt : null;
  if (elt instanceof HTMLFormElement) {
    return elt;
  }
  if (elt && typeof elt.closest === "function") {
    return elt.closest("form");
  }
  if (event && event.target instanceof HTMLFormElement) {
    return event.target;
  }
  return null;
}

function clearFlashQueryParams() {
  const url = new URL(window.location.href);
  let changed = false;
  ["flash_kind", "flash_title", "flash_body"].forEach((key) => {
    if (url.searchParams.has(key)) {
      url.searchParams.delete(key);
      changed = true;
    }
  });
  if (changed) {
    window.history.replaceState({}, "", url.toString());
  }
}

function hasVisibleOverlay() {
  return Array.from(document.querySelectorAll("[data-admin-overlay], .hs-overlay")).some((overlay) => {
    if (!(overlay instanceof HTMLElement)) {
      return false;
    }
    return !overlay.classList.contains("hidden") && overlay.getAttribute("aria-hidden") !== "true";
  });
}

function restoreOverlayPageState() {
  if (hasVisibleOverlay()) {
    return;
  }

  document.documentElement.style.removeProperty("overflow");
  document.documentElement.style.removeProperty("padding-right");
  document.body.style.removeProperty("overflow");
  document.body.style.removeProperty("padding-right");

  document.documentElement.classList.remove("overflow-hidden", "hs-overlay-body-open");
  document.body.classList.remove("overflow-hidden", "hs-overlay-body-open");

  document.querySelectorAll(".hs-overlay-backdrop").forEach((backdrop) => backdrop.remove());
}

function toastKind(kind) {
  const normalized = String(kind || "").trim();
  if (normalized === "error" || normalized === "warn") {
    return normalized;
  }
  return "success";
}

function toastRole(kind) {
  return kind === "error" || kind === "warn" ? "alert" : "status";
}

function toastLive(kind) {
  return kind === "error" || kind === "warn" ? "assertive" : "polite";
}

function toastViewport() {
  let viewport = document.querySelector("[data-admin-toast-viewport]");
  if (viewport instanceof HTMLElement) {
    return viewport;
  }

  viewport = document.createElement("div");
  viewport.setAttribute("data-admin-toast-viewport", "true");
  viewport.className =
    "pointer-events-none fixed inset-x-4 top-4 z-[80] flex flex-col items-end gap-3 sm:inset-x-auto sm:right-6 sm:top-6";
  document.body.appendChild(viewport);
  return viewport;
}

function toastIconSVG(kind) {
  switch (kind) {
    case "error":
      return `<svg class="size-4" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="10" cy="10" r="7"></circle><path d="m7.4 7.4 5.2 5.2"></path><path d="m12.6 7.4-5.2 5.2"></path></svg>`;
    case "warn":
      return `<svg class="size-4" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M10 3.5 17 16.5H3L10 3.5Z"></path><path d="M10 8v3.5"></path><circle cx="10" cy="14.1" r=".9" fill="currentColor" stroke="none"></circle></svg>`;
    default:
      return `<svg class="size-4" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m5.5 10 3 3 6-6"></path><circle cx="10" cy="10" r="7"></circle></svg>`;
  }
}

function createToastElement(detail) {
  const kind = toastKind(detail.kind);
  const title = String(detail.title || "").trim();
  if (!title) {
    return null;
  }

  const body = String(detail.body || "").trim();
  const element = document.createElement("div");
  element.setAttribute("data-admin-toast", "true");
  element.setAttribute("data-admin-toast-kind", kind);
  element.setAttribute("role", toastRole(kind));
  element.setAttribute("aria-live", toastLive(kind));
  element.setAttribute("aria-atomic", "true");
  element.className = `${TOAST_CLASS_BY_KIND[kind]} transform transition duration-200 ease-out motion-reduce:transition-none`;

  const bodyMarkup = body ? `<div class="mt-1 text-sm leading-6 text-slate-600">${escapeHTML(body)}</div>` : "";
  element.innerHTML = `
    <div class="flex items-start gap-3">
      <div class="${TOAST_ICON_CLASS_BY_KIND[kind]}">
        ${toastIconSVG(kind)}
      </div>
      <div class="min-w-0 flex-1">
        <div class="text-[15px] font-extrabold leading-6 tracking-[-0.01em] text-slate-950">${escapeHTML(title)}</div>
        ${bodyMarkup}
      </div>
      <button type="button" data-admin-toast-close="true" class="${TOAST_CLOSE_CLASS_BY_KIND[kind]}" aria-label="关闭提示">
        <svg class="size-4" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" aria-hidden="true">
          <path d="m6 6 8 8"></path>
          <path d="m14 6-8 8"></path>
        </svg>
      </button>
    </div>
  `;
  return element;
}

function animateToastIn(element) {
  if (!(element instanceof HTMLElement) || REDUCED_MOTION.matches) {
    return;
  }
  element.style.opacity = "0";
  element.style.transform = "translate3d(0, 12px, 0)";
  window.requestAnimationFrame(() => {
    window.requestAnimationFrame(() => {
      element.style.opacity = "1";
      element.style.transform = "translate3d(0, 0, 0)";
    });
  });
}

function dismissToast(element) {
  if (!(element instanceof HTMLElement) || element.dataset.dismissing === "true") {
    return;
  }

  element.dataset.dismissing = "true";
  window.clearTimeout(Number(element.dataset.toastTimer || "0"));

  if (REDUCED_MOTION.matches) {
    element.remove();
    return;
  }

  element.style.opacity = "0";
  element.style.transform = "translate3d(0, -10px, 0)";
  window.setTimeout(() => {
    element.remove();
  }, 220);
}

function scheduleToastDismiss(element, delay = DEFAULT_TOAST_DELAY) {
  if (!(element instanceof HTMLElement)) {
    return;
  }
  window.clearTimeout(Number(element.dataset.toastTimer || "0"));
  const timer = window.setTimeout(() => dismissToast(element), delay);
  element.dataset.toastTimer = String(timer);
}

function bindToast(element) {
  if (!(element instanceof HTMLElement) || element.dataset.toastBound === "true") {
    return;
  }

  element.dataset.toastBound = "true";
  animateToastIn(element);
  scheduleToastDismiss(element);

  element.addEventListener("mouseenter", () => {
    window.clearTimeout(Number(element.dataset.toastTimer || "0"));
  });
  element.addEventListener("mouseleave", () => {
    scheduleToastDismiss(element, 2600);
  });

  const closeButton = element.querySelector("[data-admin-toast-close]");
  if (closeButton instanceof HTMLButtonElement) {
    closeButton.addEventListener("click", () => dismissToast(element));
  }
}

function hydrateExistingToasts() {
  document.querySelectorAll("[data-admin-toast]").forEach((toast) => {
    bindToast(toast);
  });
}

function extractToastDetail(event) {
  if (!(event instanceof CustomEvent)) {
    return null;
  }
  const detail = event.detail;
  if (detail && typeof detail === "object") {
    if (detail.value && typeof detail.value === "object") {
      return detail.value;
    }
    if ("title" in detail || "body" in detail || "kind" in detail) {
      return detail;
    }
  }
  return null;
}

function showAdminToast(detail) {
  const toast = createToastElement(detail || {});
  if (!(toast instanceof HTMLElement)) {
    return;
  }
  toastViewport().appendChild(toast);
  bindToast(toast);
}

function adminActionDialog() {
  return document.getElementById("admin-action-dialog");
}

function resetSharedActionDialog() {
  const dialog = adminActionDialog();
  if (!(dialog instanceof HTMLElement)) {
    return;
  }

  const form = dialog.querySelector("[data-admin-action-dialog-form]");
  if (form instanceof HTMLFormElement) {
    resetFormSubmitting(form);
  }
}

function closeSharedActionDialog() {
  const dialog = adminActionDialog();
  if (!(dialog instanceof HTMLElement)) {
    return;
  }

  dialog.classList.add("hidden");
  dialog.classList.remove("open", "opened");
  dialog.setAttribute("aria-hidden", "true");
  dialog.removeAttribute("open");

  window.requestAnimationFrame(() => {
    window.requestAnimationFrame(() => {
      restoreOverlayPageState();
      resetSharedActionDialog();
    });
  });
}

function actionReturnTo() {
  return `${window.location.pathname}${window.location.search}`;
}

function actionDialogChipClass(kind) {
  return ACTION_CHIP_CLASS_BY_KIND[String(kind || "").trim()] || ACTION_CHIP_CLASS_BY_KIND.neutral;
}

function setTextBlock(element, text) {
  if (!(element instanceof HTMLElement)) {
    return;
  }

  const value = String(text || "").trim();
  element.textContent = value;
  element.classList.toggle("hidden", value === "");
}

function populateActionDialogChips(container, chips) {
  if (!(container instanceof HTMLElement)) {
    return;
  }
  container.innerHTML = "";

  if (!Array.isArray(chips) || chips.length === 0) {
    container.classList.add("hidden");
    return;
  }

  chips.forEach((chip) => {
    const label = String(chip && chip.label ? chip.label : "").trim();
    if (!label) {
      return;
    }
    const element = document.createElement("span");
    element.className = actionDialogChipClass(chip.kind);
    element.textContent = label;
    container.appendChild(element);
  });

  container.classList.toggle("hidden", container.childElementCount === 0);
}

function populateActionDialogFields(container, fields) {
  if (!(container instanceof HTMLElement)) {
    return;
  }
  container.innerHTML = "";

  if (!Array.isArray(fields) || fields.length === 0) {
    container.classList.add("hidden");
    return;
  }

  fields.forEach((field) => {
    const label = String(field && field.label ? field.label : "").trim();
    const value = String(field && field.value ? field.value : "").trim();
    if (!label && !value) {
      return;
    }

    const card = document.createElement("div");
    card.className = "rounded-[1.25rem] bg-slate-50/90 p-4 ring-1 ring-slate-200/80";
    card.innerHTML = `
      <strong class="text-xs font-extrabold uppercase tracking-[0.1em] text-slate-500">${escapeHTML(label || "内容")}</strong>
      <div class="mt-2 whitespace-pre-line break-words text-sm leading-7 text-slate-700">${escapeHTML(value || "-")}</div>
    `;
    container.appendChild(card);
  });

  container.classList.toggle("hidden", container.childElementCount === 0);
}

function populateActionDialogHiddenInputs(container, hiddenFields) {
  if (!(container instanceof HTMLElement)) {
    return;
  }
  container.innerHTML = "";

  if (!Array.isArray(hiddenFields)) {
    return;
  }

  hiddenFields.forEach((field) => {
    const name = String(field && field.name ? field.name : "").trim();
    if (!name) {
      return;
    }
    const input = document.createElement("input");
    input.type = "hidden";
    input.name = name;
    input.value = String(field && field.value ? field.value : "");
    container.appendChild(input);
  });
}

function populateSharedActionDialog(payload) {
  const dialog = adminActionDialog();
  if (!(dialog instanceof HTMLElement) || !payload || typeof payload !== "object") {
    return;
  }

  const title = dialog.querySelector("[data-admin-action-dialog-title]");
  const body = dialog.querySelector("[data-admin-action-dialog-body]");
  const spotlight = dialog.querySelector("[data-admin-action-dialog-spotlight]");
  const spotlightText = dialog.querySelector("[data-admin-action-dialog-spotlight-text]");
  const chips = dialog.querySelector("[data-admin-action-dialog-chips]");
  const fields = dialog.querySelector("[data-admin-action-dialog-fields]");
  const hidden = dialog.querySelector("[data-admin-action-dialog-hidden]");
  const form = dialog.querySelector("[data-admin-action-dialog-form]");
  const submit = dialog.querySelector("[data-admin-action-dialog-submit]");

  if (title instanceof HTMLElement) {
    title.textContent = String(payload.title || "").trim() || "确认操作";
  }
  setTextBlock(body, payload.body);

  if (spotlight instanceof HTMLElement && spotlightText instanceof HTMLElement) {
    const text = String(payload.spotlight || "").trim();
    spotlightText.textContent = text;
    spotlight.classList.toggle("hidden", text === "");
  }

  populateActionDialogChips(chips, payload.chips);
  populateActionDialogFields(fields, payload.fields);
  populateActionDialogHiddenInputs(hidden, payload.hidden);

  if (form instanceof HTMLFormElement) {
    resetFormSubmitting(form);
    const endpoint = String(payload.endpoint || "").trim();
    form.action = endpoint;
    form.setAttribute("hx-post", endpoint);
    const returnInput = form.querySelector('input[name="return_to"]');
    if (returnInput instanceof HTMLInputElement) {
      returnInput.value = actionReturnTo();
    }
  }

  if (submit instanceof HTMLButtonElement) {
    submit.className = String(payload.submitClass || "").trim() || DEFAULT_ACTION_SUBMIT_CLASS;
    submit.textContent = String(payload.submitLabel || "").trim() || "确认";
    submit.setAttribute("data-loading-label", String(payload.busyLabel || payload.submitLabel || "处理中"));
  }
}

function parseActionPayload(trigger) {
  const raw = trigger.getAttribute("data-admin-action");
  if (!raw) {
    return null;
  }

  try {
    return JSON.parse(raw);
  } catch (error) {
    console.error("Failed to parse admin action payload", error);
    return null;
  }
}

function bindAdminEvent(name, handler) {
  document.addEventListener(name, (event) => handler(event));
}

function adminSubmitChoiceFilter(input) {
  if (!(input instanceof HTMLInputElement) || !(input.form instanceof HTMLFormElement)) {
    return;
  }

  const form = input.form;
  const hxGet = String(form.getAttribute("hx-get") || "").trim();
  if (window.htmx && hxGet) {
    const url = new URL(hxGet, window.location.origin);
    const query = new URLSearchParams(new FormData(form)).toString();
    const target = String(form.getAttribute("hx-target") || "").trim() || "#admin-page-body";
    const swap = String(form.getAttribute("hx-swap") || "").trim() || "outerHTML";
    const requestPath = query ? `${url.pathname}?${query}` : url.pathname;

    window.htmx.ajax("GET", requestPath, {
      source: form,
      target,
      swap,
      pushURL: form.getAttribute("hx-push-url") === "true",
    });
    return;
  }

  if (typeof form.requestSubmit === "function") {
    form.requestSubmit();
    return;
  }

  form.submit();
}

function escapeHTML(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

document.addEventListener("DOMContentLoaded", () => {
  Alpine.start();
  initAdminInteractions();
  clearFlashQueryParams();
  restoreOverlayPageState();
  hydrateExistingToasts();
});

document.addEventListener("htmx:afterSwap", () => {
  initAdminInteractions();
  restoreOverlayPageState();
  hydrateExistingToasts();
});

document.addEventListener("pageshow", () => {
  restoreOverlayPageState();
});

document.addEventListener(
  "click",
  (event) => {
    const target = event.target instanceof Element ? event.target : null;
    if (!target) {
      return;
    }

    const actionTrigger = target.closest("[data-admin-action]");
    if (actionTrigger instanceof HTMLElement) {
      populateSharedActionDialog(parseActionPayload(actionTrigger));
    }

    const overlayTrigger = target.closest("[data-hs-overlay]");
    if (!overlayTrigger) {
      return;
    }

    window.requestAnimationFrame(() => {
      window.requestAnimationFrame(() => {
        restoreOverlayPageState();
      });
    });
  },
  true,
);

document.addEventListener(
  "submit",
  (event) => {
    const form = event.target;
    if (!(form instanceof HTMLFormElement)) {
      return;
    }
    if (form.dataset.submitting === "true") {
      event.preventDefault();
      return;
    }
    let submitter = event.submitter instanceof HTMLButtonElement ? event.submitter : null;
    if (!submitter && document.activeElement instanceof HTMLButtonElement && document.activeElement.form === form) {
      submitter = document.activeElement;
    }
    markFormSubmitting(form, submitter);
  },
  true,
);

document.addEventListener("htmx:afterRequest", (event) => {
  resetFormSubmitting(formFromEvent(event));
});

document.addEventListener("htmx:responseError", (event) => {
  resetFormSubmitting(formFromEvent(event));
});

document.addEventListener("htmx:sendError", (event) => {
  resetFormSubmitting(formFromEvent(event));
});

bindAdminEvent("admin:toast", (event) => {
  const detail = extractToastDetail(event);
  if (detail) {
    showAdminToast(detail);
  }
});

bindAdminEvent("admin:action-dialog-close", () => {
  closeSharedActionDialog();
});

window.adminSubmitChoiceFilter = adminSubmitChoiceFilter;
