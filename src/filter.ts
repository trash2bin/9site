import type { Availability, CriterionDefinition, FilterState } from "./criteria.js";
import type { Photo } from "./model.js";

function matches(photo: Photo, state: FilterState, criteria: readonly CriterionDefinition[]): boolean {
  return criteria.every(({ key, valuesOf }) => {
    const selected = state[key as keyof FilterState] ?? [];
    if (selected.length === 0) return true;

    const values = valuesOf(photo);
    return selected.some((value) => values.includes(value));
  });
}

export function applyFilters(photos: Photo[], state: FilterState, criteria: readonly CriterionDefinition[]): Photo[] {
  return photos.filter((photo) => matches(photo, state, criteria));
}

/**
 * Для каждого варианта проверяет, найдётся ли снимок при выборах в остальных
 * группах; результат используется для disabled.
 */
export function getAvailability(
  photos: Photo[],
  state: FilterState,
  criteria: readonly CriterionDefinition[]
): Availability {
  const availability: Availability = {};

  for (const criterion of criteria) {
    const key = criterion.key as keyof FilterState;
    availability[key] = new Set(
      criterion.options
        .filter(({ value }) => {
          const probe: FilterState = { ...state, [key]: [value] };
          return photos.some((photo) => matches(photo, probe, criteria));
        })
        .map(({ value }) => value)
    );
  }

  return availability;
}
