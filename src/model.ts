/** Запись фотоальбома: сервер отдаёт готовый путь к файлу в поле url. */

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

/** Подписи разделов: список разделов приходит из базы, подписи живут здесь. */
const ALBUM_LABELS: Record<string, string> = {
  gadgets: "техника",
  garage: "гараж",
  mascots: "tux",
  music: "музыка",
  misc: "прочее",
};

/** Новый раздел без подписи показывается своим слагом, а не пропадает. */
export function albumLabel(slug: string): string {
  return ALBUM_LABELS[slug] ?? slug;
}

export type FileType = (typeof TYPE_OPTIONS)[number]["value"];
export type Orientation = (typeof ORIENTATION_OPTIONS)[number]["value"];

export interface Photo {
  name: string;
  /** Путь от корня сайта: /assets/img/x.jpg или /photos/p...webp. */
  url: string;
  album: string;
  width: number;
  height: number;
}
