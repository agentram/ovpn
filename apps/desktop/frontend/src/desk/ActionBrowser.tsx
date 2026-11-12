import { useState } from "react";
import type { Action } from "./api";
import { actionGroups, actionGroup } from "./actions";
import type { ActionGroup } from "./actions";
import { actionName } from "./i18n";
import type { Language } from "./i18n";
import { uiCopy } from "./uiCopy";

export function ActionBrowser({
  actions,
  language,
  onChoose,
}: {
  actions: Action[];
  language: Language;
  onChoose: (action: Action) => void;
}) {
  const t = uiCopy(language);
  const [query, setQuery] = useState("");
  const [group, setGroup] = useState<ActionGroup>(
    actionGroups.find((group) =>
      actions.some((action) => actionGroup(action) === group),
    ) ?? "system",
  );
  const groups = actionGroups.filter((group) =>
    actions.some((action) => actionGroup(action) === group),
  );
  const search = query.trim().toLowerCase();
  const matching = actions.filter((action) =>
    search
      ? `${actionName(action.id, action.title, language)} ${action.id}`
          .toLowerCase()
          .includes(search)
      : actionGroup(action) === group,
  );
  return (
    <section className="desk-action-browser">
      <input
        type="search"
        aria-label={t.search}
        placeholder={t.search}
        value={query}
        onChange={(e) => setQuery(e.target.value)}
      />
      <div className="desk-action-layout">
        <div
          className="desk-action-categories"
          role="group"
          aria-label={t.more}
        >
          {groups.map((id) => (
            <button
              key={id}
              type="button"
              aria-pressed={!search && id === group}
              onClick={() => {
                setQuery("");
                setGroup(id);
              }}
            >
              {t[id]}
            </button>
          ))}
        </div>
        <div className="desk-action-list">
          {matching.length ? (
            matching.map((action) => (
              <button
                type="button"
                key={action.id}
                className={action.destructive ? "danger-text" : ""}
                onClick={() => onChoose(action)}
              >
                <span>{actionName(action.id, action.title, language)}</span>
                <span aria-hidden>›</span>
              </button>
            ))
          ) : (
            <p>{t.noActions}</p>
          )}
        </div>
      </div>
    </section>
  );
}
