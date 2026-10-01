/** Единое описание критериев: его обходят фильтр и интерфейс. */

import { fileTypeOf, orientationOf } from "./media.js";
import type { Photo } from "./model.js";
import { ALBUM_OPTIONS, ORIENTATION_OPTIONS, TYPE_OPTIONS } from "./model.js";

export interface CriterionDefinition {
  key: string;
  title: string;
  options: readonly { value: string; label: string }[];
  valueOf: (photo: Photo) => string;
}

export const CRITERIA = [
  { key: "album", title: "Раздел", options: ALBUM_OPTIONS, valueOf: (photo: Photo) => photo.album },
  { key: "type", title: "Тип файла", options: TYPE_OPTIONS, valueOf: fileTypeOf },
  { key: "orientation", title: "Ориентация", options: ORIENTATION_OPTIONS, valueOf: orientationOf },
] as const satisfies readonly CriterionDefinition[];

export type CriterionKey = (typeof CRITERIA)[number]["key"];
export type FilterState = Partial<Record<CriterionKey, string[]>>;
export type Availability = Partial<Record<CriterionKey, Set<string>>>;

/** Проверяет, что строка — имя описанного критерия. */
export function isCriterionKey(value: string): value is CriterionKey {
  return CRITERIA.some(({ key }) => key === value);
}
