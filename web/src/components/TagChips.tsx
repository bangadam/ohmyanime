export interface TagChipsProps {
  label: string;
  items: string[];
  onSelect: (value: string) => void;
  labelFor: (value: string) => string;
}

/**
 * A taxonomy facet group (genres or tags) on the details page.
 * Each chip is a plain button: its accessible name explains the navigation
 * ("Filter by Action") while the visible text stays the bare facet value.
 */
export function TagChips({ label, items, onSelect, labelFor }: TagChipsProps) {
  if (items.length === 0) return null;
  return (
    <div className="detail-taxonomy" role="group" aria-label={label}>
      <span className="detail-taxonomy__label" aria-hidden="true">
        {label}
      </span>
      {items.map((item) => (
        <button
          key={item}
          type="button"
          className="taxonomy-chip"
          title={labelFor(item)}
          aria-label={labelFor(item)}
          onClick={() => onSelect(item)}
        >
          {item}
        </button>
      ))}
    </div>
  );
}
