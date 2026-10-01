/** Рендер карточек фотоальбома и контролов фильтра. */

import { CRITERIA, isCriterionKey, type Availability, type FilterState } from "./criteria.js";
import type { Photo } from "./model.js";

/** Экранирует данные перед вставкой в HTML. */
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

/** Строит карточку снимка. Фильтруемые значения читаются из Photo, не из DOM. */
function photoCard(photo: Photo): string {
  return `<figure class="part-card photo-card">
      <img src="/assets/img/${escapeHtml(photo.src)}" alt="${escapeHtml(photo.name)}" loading="lazy" decoding="async" />
      <figcaption data-file="${escapeHtml(photo.src)}">${escapeHtml(photo.name)}</figcaption>
    </figure>`;
}

/** Отрисовывает только подходящие карточки. */
export function renderPhotos(photos: Photo[], selector: string): void {
  const host = document.querySelector(selector);
  if (!host) return;

  host.innerHTML = photos.length
    ? photos.map(photoCard).join("\n")
    : `<p class="catalog-empty">[ под такие критерии ничего не нашлось // сбрось фильтр ]</p>`;
}

/** Создаёт группы фильтра из того же описания, что использует логика. */
export function renderFilters(selector: string): void {
  const host = document.querySelector(selector);
  if (!host) return;

  host.innerHTML = CRITERIA.map(
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
  ).join("\n");
}

/** Обновляет выбор и доступность, не заменяя DOM-узлы. */
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

/** Собирает состояние из отмеченных чекбоксов. */
export function readState(selector: string): FilterState {
  const result: FilterState = {};
  document.querySelectorAll<HTMLInputElement>(`${selector} input[type=checkbox]:checked`).forEach((box) => {
    if (isCriterionKey(box.name)) result[box.name] = [...(result[box.name] ?? []), box.value];
  });
  return result;
}

/** Обновляет счётчик результатов. */
export function renderCount(found: number, total: number, selector: string): void {
  const host = document.querySelector(selector);
  if (host) host.textContent = `// найдено ${found} из ${total}`;
}
