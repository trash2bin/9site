/** Загрузка снимков с сервера: база источник правды, форма ответа из OpenAPI. */

import { paths } from "./api-types.js";
import type { components } from "./api-types.js";
import type { Album, Photo } from "./model.js";

export type AlbumData = components["schemas"]["PhotosResponse"];

function isAlbum(value: unknown): value is Album {
  const album = value as Partial<Album> | null;
  return (
    typeof album === "object" && album !== null && typeof album.slug === "string" && typeof album.label === "string"
  );
}

function isPhoto(value: unknown): value is Photo {
  const photo = value as Partial<Photo> | null;
  return (
    typeof photo === "object" &&
    photo !== null &&
    typeof photo.name === "string" &&
    typeof photo.url === "string" &&
    typeof photo.album === "string" &&
    typeof photo.width === "number" &&
    typeof photo.height === "number" &&
    typeof photo.author === "string" &&
    Array.isArray(photo.tags) &&
    photo.tags.every((tag) => typeof tag === "string")
  );
}

/** Бросает при любой ошибке: витрина покажет «база не ответила», а не пустоту. */
export async function loadPhotos(): Promise<AlbumData> {
  const response = await fetch(paths.photos, { headers: { Accept: "application/json" } });
  if (!response.ok) {
    throw new Error(`${paths.photos}: ${response.status}`);
  }

  const data = (await response.json()) as Partial<AlbumData>;
  if (
    !Array.isArray(data.photos) ||
    !data.photos.every(isPhoto) ||
    !Array.isArray(data.albums) ||
    !data.albums.every(isAlbum)
  ) {
    throw new Error(`${paths.photos}: неожиданный ответ`);
  }

  return {
    photos: data.photos,
    albums: data.albums,
    tags: (Array.isArray(data.tags) ? data.tags : []).filter((tag) => typeof tag === "string"),
  };
}
