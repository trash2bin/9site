/**
 * Версия для печати: переключение стилевых файлов без перезагрузки.
 *
 * В <head> лежат два <link>: обычный print-off.css (media="all") и
 * print-on.css (media="all" disabled). Клик по кнопке меняет у них
 * атрибут disabled — браузер перечитывает каскад на месте, страница не
 * перезагружается, состояние фильтра не теряется.
 *
 * Обычный @media print остался в css/responsive.css и работает при печати
 * поверх любого режима: он не мешает ручному переключению, а страхует на
 * случай, когда пользователь печатает без нажатия кнопки.
 */
/** id ссылок на стили печати, порядок важен: сначала выключаем обычный. */
const PRINT_OFF_ID = "style-print-off";
const PRINT_ON_ID = "style-print-on";
/** Кнопки переключения и подписи в двух состояниях. */
const LABELS = {
    off: "версия для печати",
    on: "обычная версия",
};
/**
 * Включает или выключает печатную тему.
 *
 * Пока идёт переключение, оба <link> на миг активны, поэтому сначала
 * выключаем прежний и только потом включаем новый: иначе правила печати
 * успели бы примениться поверх обычных и страница дёрнулась бы.
 */
function setPrintMode(on) {
    const off = document.getElementById(PRINT_OFF_ID);
    const onLink = document.getElementById(PRINT_ON_ID);
    if (off) {
        off.disabled = on;
    }
    if (onLink) {
        onLink.disabled = !on;
    }
    document.body.classList.toggle("print-mode", on);
    document.querySelectorAll("[data-print-toggle]").forEach((button) => {
        button.textContent = on ? LABELS.on : LABELS.off;
        button.setAttribute("aria-pressed", String(on));
    });
}
/**
 * Навешивает переключение на все кнопки с data-print-toggle, запуск печати
 * на кнопки с data-print-now и сразу выставляет обычный режим, чтобы
 * подпись совпадала с состоянием.
 */
export function initPrintMode() {
    document.querySelectorAll("[data-print-toggle]").forEach((button) => {
        button.addEventListener("click", () => {
            setPrintMode(!document.body.classList.contains("print-mode"));
        });
    });
    // «Распечатать» — тонкая надстройка над визуальным режимом: браузер сам
    // применит @media print из print.css, поэтому диалог печати покажет уже
    // печатный вид. Если страницу не отрисовали в печатном режиме заранее —
    // включаем его, иначе пользователь может отправить на бумагу цветной
    // вариант с шапкой и панелью фильтра.
    document.querySelectorAll("[data-print-now]").forEach((button) => {
        button.addEventListener("click", () => {
            if (!document.body.classList.contains("print-mode")) {
                setPrintMode(true);
            }
            window.print();
        });
    });
    setPrintMode(false);
}
