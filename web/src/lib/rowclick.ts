/** Elements that handle their own clicks inside a clickable row. */
const interactive =
	'a, button, input, select, textarea, label, summary, [role="group"], [data-no-row-click]';

/**
 * Click handler for a table: a click anywhere in a row follows the row's `a.row-link`.
 * Modifier and middle clicks open a new tab like they would on the link; selecting text does nothing.
 */
export function rowClick(e: MouseEvent) {
	if (e.button > 1) return;
	const target = e.target as Element | null;
	if (!target || target.closest(interactive)) return;
	const row = target.closest('tbody tr');
	const link = row?.querySelector<HTMLAnchorElement>('a.row-link');
	if (!link) return;
	if (window.getSelection()?.toString()) return;
	if (e.button === 1 || e.metaKey || e.ctrlKey) {
		window.open(link.href, '_blank', 'noopener');
		return;
	}
	link.click();
}
