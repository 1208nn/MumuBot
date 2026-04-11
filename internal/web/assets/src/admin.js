import htmx from "htmx.org";
import Alpine from "alpinejs";
import { HSOverlay, HSStaticMethods } from "preline";
import Toastify from "toastify-js";

window.htmx = htmx;
window.Alpine = Alpine;

const DEFAULT_TOAST_DELAY = 4200;
const REDUCED_MOTION = window.matchMedia("(prefers-reduced-motion: reduce)");

function initAdminInteractions() {
  if (HSStaticMethods && typeof HSStaticMethods.autoInit === "function") {
    HSStaticMethods.autoInit(["overlay"]);
  }
}

function markFormSubmitting(form, activeButton) {
  if (!(form instanceof HTMLFormElement)) {
    return;
  }

  form.dataset.submitting = "true";
  const buttons = form.querySelectorAll('button[type="submit"]');
  buttons.forEach((button) => {
    button.disabled = true;
    button.classList.add("opacity-70", "cursor-wait");
  });

  if (activeButton instanceof HTMLButtonElement) {
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

function normalizeToastDetail(detail) {
  if (!detail || typeof detail !== "object") {
    return null;
  }

  const normalized = {
    kind: String(detail.kind || detail.Kind || "").trim(),
    title: String(detail.title || detail.Title || "").trim(),
    body: String(detail.body || detail.Body || "").trim(),
  };

  if (!normalized.title) {
    return null;
  }

  return normalized;
}

function readInitialToast() {
  const payload = document.getElementById("admin-flash-payload");
  if (payload instanceof HTMLMetaElement) {
    try {
      return normalizeToastDetail(JSON.parse(payload.content || "{}"));
    } catch {
      return null;
    } finally {
      payload.remove();
    }
  }

  if (payload instanceof HTMLScriptElement) {
    try {
      return normalizeToastDetail(JSON.parse(payload.textContent || "{}"));
    } catch {
      return null;
    } finally {
      payload.remove();
    }
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

function toastIcon(kind) {
  switch (kind) {
    case "error":
      return '<svg class="size-4" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="10" cy="10" r="6.5"></circle><path d="M10 6.5v4.5"></path><path d="M10 13.5h.01"></path></svg>';
    case "warn":
      return '<svg class="size-4" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M10 3.5 17 16.5H3L10 3.5Z"></path><path d="M10 7.5v4"></path><path d="M10 13.5h.01"></path></svg>';
    default:
      return '<svg class="size-4" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="m5.5 10 3 3 6-6"></path></svg>';
  }
}

function buildToastNode(detail) {
  const normalized = normalizeToastDetail(detail);
  if (!normalized) {
    return null;
  }

  const kind = toastKind(normalized.kind);
  const body = normalized.body;
  const wrapper = document.createElement("div");
  wrapper.className = "admin-toast";
  wrapper.dataset.kind = kind;
  wrapper.setAttribute("role", toastRole(kind));
  wrapper.setAttribute("aria-live", toastLive(kind));
  wrapper.setAttribute("aria-atomic", "true");
  wrapper.innerHTML = `
    <div class="admin-toast__icon">${toastIcon(kind)}</div>
    <div class="admin-toast__content">
      <div data-admin-toast-title class="text-[15px] font-extrabold leading-6 tracking-[-0.01em] text-slate-950"></div>
      <div class="admin-toast__body text-sm leading-6 text-slate-600"></div>
    </div>
    <button type="button" class="admin-toast__close" aria-label="关闭提示">
      <svg class="size-4" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round">
        <path d="m6 6 8 8"></path>
        <path d="m14 6-8 8"></path>
      </svg>
    </button>
  `;

  const title = wrapper.querySelector("[data-admin-toast-title]");
  if (title instanceof HTMLElement) {
    title.textContent = normalized.title;
  }

  const bodyNode = wrapper.querySelector(".admin-toast__body");
  if (bodyNode instanceof HTMLElement) {
    bodyNode.textContent = body;
    bodyNode.classList.toggle("hidden", body === "");
  }

  return wrapper;
}

function toastOffset() {
  if (window.matchMedia("(max-width: 639px)").matches) {
    return { x: 16, y: 16 };
  }
  return { x: 24, y: 24 };
}

function showAdminToast(detail) {
  const node = buildToastNode(detail);
  if (!(node instanceof HTMLElement)) {
    return;
  }

  const toast = Toastify({
    node,
    gravity: "top",
    position: "right",
    offset: toastOffset(),
    duration: DEFAULT_TOAST_DELAY,
    stopOnFocus: true,
    close: false,
    className: REDUCED_MOTION.matches ? "admin-toastify admin-toastify-static" : "admin-toastify",
  });
  toast.showToast();

  const closeButton = node.querySelector(".admin-toast__close");
  if (closeButton instanceof HTMLButtonElement) {
    closeButton.addEventListener("click", (event) => {
      event.preventDefault();
      toast.hideToast();
    });
  }
}

function closeOverlay(selector) {
  if (HSOverlay && typeof HSOverlay.close === "function") {
    HSOverlay.close(selector);
  }
}

function isSharedActionDialogForm(form) {
  return form instanceof HTMLFormElement && form.hasAttribute("data-admin-action-dialog-form");
}

function extractToastDetail(event) {
  if (!(event instanceof CustomEvent)) {
    return null;
  }
  const detail = event.detail;
  if (detail && typeof detail === "object") {
    if (detail.value && typeof detail.value === "object") {
      return normalizeToastDetail(detail.value);
    }
    if ("title" in detail || "body" in detail || "kind" in detail) {
      return normalizeToastDetail(detail);
    }
  }
  return null;
}

function bootAdminPage() {
  initAdminInteractions();
  Alpine.start();
  const detail = readInitialToast();
  if (detail) {
    showAdminToast(detail);
  }
  clearFlashQueryParams();
}

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", bootAdminPage, { once: true });
} else {
  queueMicrotask(bootAdminPage);
}

document.addEventListener("htmx:afterSwap", () => {
  initAdminInteractions();
});

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

    if (isSharedActionDialogForm(form)) {
      const actionKind = form.querySelector('input[name="action_kind"]');
      if (!(actionKind instanceof HTMLInputElement) || String(actionKind.value || "").trim() === "") {
        event.preventDefault();
        resetFormSubmitting(form);
        return;
      }
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

document.addEventListener("admin:toast", (event) => {
  const detail = extractToastDetail(event);
  if (detail) {
    showAdminToast(detail);
  }
});

document.addEventListener("admin:action-dialog-close", () => {
  closeOverlay("#admin-action-dialog");
});
