/** Форма кадра и раздела приходит из OpenAPI: src/api-types.d.ts, npm run api:gen. */

import type { components } from "./api-types.js";

export const TYPE_OPTIONS = [
  { value: "photo", label: "фото" },
  { value: "gif", label: "анимация" },
  { value: "vector", label: "вектор" },
] as const;

export const ORIENTATION_OPTIONS = [
  { value: "wide", label: "широкое" },
  { value: "square", label: "квадратное" },
  { value: "tall", label: "высокое" },
] as const;

/** Раздел альбома: слаг и подпись. Список фиксирован в album.Sections на сервере. */
export type Album = components["schemas"]["Section"];

export type FileType = (typeof TYPE_OPTIONS)[number]["value"];
export type Orientation = (typeof ORIENTATION_OPTIONS)[number]["value"];

/** Запись альбома: url это путь от корня сайта, author у засева system. */
export type Photo = components["schemas"]["Photo"];
