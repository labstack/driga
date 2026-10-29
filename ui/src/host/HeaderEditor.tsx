import { ActionIcon, Button, Combobox, Group, Menu, Text, useCombobox } from "@mantine/core";
import { DotsThree, Plus } from "@phosphor-icons/react";
import { useEffect, useState } from "react";
import type { Panel } from "../panels/types";
import { dashboards } from "./api";

/** The catalog only suggests widgets absent from the current spec. */
export function HeaderEditor({ panels, busy, onAdd, onReset, onAsk }: { panels: Panel[]; busy: boolean; onAdd: (panel: Panel) => void; onReset: () => void; onAsk: (draft: string) => void }) {
  const [catalog, setCatalog] = useState<Panel[]>([]);
  const [query, setQuery] = useState("");
  const [problem, setProblem] = useState("");
  const loadCatalog = async () => {
    setProblem("");
    try { setCatalog(await dashboards.catalog()); }
    catch (e) { setCatalog([]); setProblem((e as Error).message); }
  };
  const store = useCombobox({ onDropdownOpen: () => { store.focusSearchInput(); void loadCatalog(); }, onDropdownClose: source => { store.resetSelectedOption(); setQuery(""); if (source === "keyboard") store.focusTarget(); } });
  const absent = catalog.filter(p => !panels.some(shown => shown.id === p.id));
  const options = absent.filter(p => p.title.toLowerCase().includes(query.toLowerCase()));
  const full = panels.length >= 16;
  useEffect(() => { if (store.dropdownOpened) store.selectFirstOption(); }, [catalog, query, store.dropdownOpened]);
  return <Group gap={2} wrap="nowrap" style={{ flex: "none" }}>
    <Combobox store={store} width={280} position="bottom-end" withinPortal onOptionSubmit={id => { const panel = absent.find(p => p.id === id); if (panel && !full && !busy) onAdd(panel); store.closeDropdown(); }}>
      <Combobox.Target targetType="button" withExpandedAttribute><ActionIcon variant="subtle" aria-label="Add widget" disabled={busy} onClick={() => store.toggleDropdown()}><Plus /></ActionIcon></Combobox.Target>
      <Combobox.Dropdown>
        <Combobox.Search aria-label="Find a widget" placeholder="Find a widget" value={query} onChange={e => { setQuery(e.currentTarget.value); }} />
        <Combobox.Options mah={320} style={{ overflowY: "auto" }}>
          {options.map(p => <Combobox.Option key={p.id} value={p.id} disabled={full || busy}>{p.title}</Combobox.Option>)}
          {options.length === 0 && <Combobox.Empty>{problem || "No matching widgets"}</Combobox.Empty>}
        </Combobox.Options>
        {full && <Text size="xs" c="dimmed" p="xs">The header shows at most 16 widgets</Text>}
        <Button variant="subtle" fullWidth size="xs" onClick={() => { store.closeDropdown(); onAsk("Add a header widget that shows "); }}>Ask Chat for another…</Button>
      </Combobox.Dropdown>
    </Combobox>
    <Menu withinPortal position="bottom-end">
      <Menu.Target><ActionIcon variant="subtle" aria-label="Header options" disabled={busy}><DotsThree /></ActionIcon></Menu.Target>
      <Menu.Dropdown><Menu.Item onClick={onReset}>Reset header</Menu.Item></Menu.Dropdown>
    </Menu>
  </Group>;
}
