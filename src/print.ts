/**
 * Версия для печати: переключение двух <link> в <head> атрибутом disabled,
 * без перезагрузки и потери состояния фильтра.
 */

/** id ссылок на стили печати, порядок важен: сначала выключаем обычный. */
const PRINT_OFF_ID = "style-print-off";
const PRINT_ON_ID = "style-print-on";

const LABELS = {
  off: "версия для печати",
  on: "обычная версия",
} as const;

/**
 * Сначала выключаем прежний <link>, потом включаем новый: иначе оба активны
 * миг и страница дёргается.
 */
function setPrintMode(on: boolean): void {
  const off = document.getElementById(PRINT_OFF_ID) as HTMLLinkElement | null;
  const onLink = document.getElementById(PRINT_ON_ID) as HTMLLinkElement | null;

  if (off) {
    off.disabled = on;
  }
  if (onLink) {
    onLink.disabled = !on;
  }

  document.body.classList.toggle("print-mode", on);

  document.querySelectorAll<HTMLButtonElement>("[data-print-toggle]").forEach((button) => {
    button.textContent = on ? LABELS.on : LABELS.off;
    button.setAttribute("aria-pressed", String(on));
  });
}

export function initPrintMode(): void {
  document.querySelectorAll<HTMLButtonElement>("[data-print-toggle]").forEach((button) => {
    button.addEventListener("click", () => {
      setPrintMode(!document.body.classList.contains("print-mode"));
    });
  });

  // Перед window.print() включаем печатную тему: иначе на бумагу уйдёт
  // цветной вариант с шапкой и панелью фильтра.
  document.querySelectorAll<HTMLButtonElement>("[data-print-now]").forEach((button) => {
    button.addEventListener("click", () => {
      if (!document.body.classList.contains("print-mode")) {
        setPrintMode(true);
      }
      window.print();
    });
  });

  setPrintMode(false);
}
