/**
 * Сгенерировано из server/internal/api/openapi.json: npm run api:types.
 * Руками не правится: источник правды, Go-структуры ответа.
 */

export interface Photo {
  /** идентификатор записи для действий владельца */
  id: number;
  /** подпись кадра */
  name: string;
  /** путь файла от корня сайта */
  url: string;
  /** слаг раздела */
  album: string;
  /** ширина файла в px */
  width: number;
  /** высота файла в px */
  height: number;
  /** логин загрузившего или system для засева */
  author: string;
  /** теги кадра по алфавиту */
  tags: string[];
}

export interface PhotosResponse {
  photos: Photo[];
  albums: Section[];
  tags: string[];
  viewer: Viewer;
}

export interface Section {
  /** слаг раздела из album.Sections */
  slug: string;
  /** подпись для интерфейса */
  label: string;
}

export interface Viewer {
  admin: boolean;
  csrf: string;
}

/** Пути API из спеки: смена адреса на сервере приезжает сюда. */
export const paths = {
  deletePhoto: "/vault/album/delete",
  photos: "/api/photos",
} as const;

export interface components {
  schemas: {
    Photo: Photo;
    PhotosResponse: PhotosResponse;
    Section: Section;
    Viewer: Viewer;
  };
}
