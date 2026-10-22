/** Рендер карточек фотоальбома и контролов фильтра. */

import { isCriterionKey, type Availability, type CriterionDefinition, type FilterState } from "./criteria.js";
import type { Photo } from "./model.js";

function escapeHtml(value: string): string {
  return value.replace(/[&<>"']/g, (char) => {
    const entities: Record<string, string> = {
      "&": "&amp;",
      "<": "&lt;",
      ">": "&gt;",
      '"': "&quot;",
      "'": "&#39;",
    };
    return entities[char];
  });
}

/** Карточка снимка: фильтруемые значения читаются из Photo, не из DOM. */
function photoCard(photo: Photo, admin: boolean): string {
  return `<figure class="part-card photo-card">
      ${admin ? `<button type="button" class="photo-delete" data-photo-delete="${photo.id}" data-photo-name="${escapeHtml(photo.name)}" aria-label="Удалить: ${escapeHtml(photo.name)}">×</button>` : ""}
      <img src="${escapeHtml(photo.url)}" alt="${escapeHtml(photo.name)}" loading="lazy" decoding="async" />
      <figcaption data-file="${escapeHtml(photo.url)}">
        ${escapeHtml(photo.name)}
        <span class="photo-author"> · ${escapeHtml(photo.author)}</span>
        ${photo.tags.length ? `<span class="photo-tags">${photo.tags.map((tag) => `#${escapeHtml(tag)}`).join(" ")}</span>` : ""}
      </figcaption>
    </figure>`;
}

export function renderPhotos(photos: Photo[], selector: string, admin: boolean): void {
  const host = document.querySelector(selector);
  if (!host) return;

  host.innerHTML = photos.length
    ? photos.map((photo) => photoCard(photo, admin)).join("\n")
    : `<p class="catalog-empty">[ под такие критерии ничего не нашлось // сбрось фильтр ]</p>`;
}

export function renderFilters(criteria: readonly CriterionDefinition[], selector: string): void {
  const host = document.querySelector(selector);
  if (!host) return;

  host.innerHTML = criteria
    .map(
      ({ key, title, options }) => `<fieldset class="filter-group">
        <legend>${escapeHtml(title)}</legend>
        ${options
          .map(
            ({ value, label }) => `<label class="filter-item">
              <input type="checkbox" name="${key}" value="${escapeHtml(value)}" />
              <span>${escapeHtml(label)}</span>
            </label>`
          )
          .join("\n")}
      </fieldset>`
    )
    .join("\n");
}

export function updateFilters(state: FilterState, available: Availability, selector: string): void {
  const host = document.querySelector(selector);
  if (!host) return;

  host.querySelectorAll<HTMLInputElement>("input[type=checkbox]").forEach((box) => {
    if (!isCriterionKey(box.name)) return;

    const selected = state[box.name]?.includes(box.value) ?? false;
    const enabled = selected || (available[box.name]?.has(box.value) ?? false);
    box.checked = selected;
    box.disabled = !enabled;
    box.closest(".filter-item")?.classList.toggle("is-off", !enabled);
  });
}

export function readState(selector: string): FilterState {
  const result: FilterState = {};
  document.querySelectorAll<HTMLInputElement>(`${selector} input[type=checkbox]:checked`).forEach((box) => {
    if (isCriterionKey(box.name)) result[box.name] = [...(result[box.name] ?? []), box.value];
  });
  return result;
}

export function renderCount(found: number, total: number, selector: string): void {
  const host = document.querySelector(selector);
  if (host) host.textContent = `// найдено ${found} из ${total}`;
}
