/** Запись фотоальбома: только хранимые данные, без вычисляемых признаков. */
export const ALBUM_OPTIONS = [
  { value: "gadgets", label: "техника" },
  { value: "garage", label: "гараж" },
  { value: "mascots", label: "tux" },
  { value: "music", label: "музыка" },
  { value: "misc", label: "прочее" },
] as const;

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

export type Album = (typeof ALBUM_OPTIONS)[number]["value"];
export type FileType = (typeof TYPE_OPTIONS)[number]["value"];
export type Orientation = (typeof ORIENTATION_OPTIONS)[number]["value"];

export interface Photo {
  name: string;
  /** Имя файла внутри public/assets/img/. */
  src: string;
  album: Album;
  width: number;
  height: number;
}
