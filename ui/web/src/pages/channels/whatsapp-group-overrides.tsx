import { useState, useEffect } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Card, CardContent } from "@/components/ui/card";
import { Plus, Trash2 } from "lucide-react";
import { useHttp } from "@/hooks/use-ws";
import type { GroupManagerGroupInfo } from "./hooks/use-channel-detail";

interface AgentData {
  id: string;
  agent_key: string;
  display_name: string;
}

export interface WhatsAppGroupConfigValues {
  require_mention?: boolean;
  agent_id?: string;
}

interface Props {
  groups: Record<string, WhatsAppGroupConfigValues>;
  onChange: (groups: Record<string, WhatsAppGroupConfigValues>) => void;
  knownGroups?: GroupManagerGroupInfo[];
}

export function WhatsAppGroupOverrides({ groups, onChange, knownGroups }: Props) {
  const http = useHttp();
  const [newGroupId, setNewGroupId] = useState("");
  const [agents, setAgents] = useState<AgentData[]>([]);

  useEffect(() => {
    http.get<{ agents: AgentData[] }>("/v1/agents").then((data) => {
      setAgents(data.agents ?? []);
    }).catch(() => {
      // Agent list will be empty — dropdown shows only "Use channel default"
    });
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const groupIds = Object.keys(groups);

  const addGroup = (id?: string) => {
    const gid = (id ?? newGroupId).trim();
    if (!gid || groups[gid]) return;
    onChange({ ...groups, [gid]: {} });
    if (!id) setNewGroupId("");
  };

  const removeGroup = (id: string) => {
    const next = { ...groups };
    delete next[id];
    onChange(next);
  };

  const updateGroup = (id: string, updates: Partial<WhatsAppGroupConfigValues>) => {
    const current = groups[id] ?? {};
    onChange({ ...groups, [id]: { ...current, ...updates } });
  };

  // Groups from manager that aren't configured yet
  const unconfiguredGroups = (knownGroups ?? []).filter(
    (g) => !groups[g.group_id],
  );

  return (
    <div className="space-y-4">
      <p className="text-sm text-muted-foreground">
        Configure per-group overrides. Each group can use a different agent and mention settings.
      </p>

      {groupIds.length === 0 && (
        <p className="text-sm text-muted-foreground italic">No group overrides configured. All groups use the channel defaults.</p>
      )}

      {groupIds.map((gid) => {
        const gc = groups[gid] ?? {};
        const knownGroup = (knownGroups ?? []).find((g) => g.group_id === gid);
        const label = gid;

        return (
          <Card key={gid}>
            <CardContent className="pt-4 space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <p className="font-medium text-sm">{label}</p>
                  {knownGroup && <p className="text-xs text-muted-foreground">Writers: {knownGroup.writer_count}</p>}
                </div>
                <Button variant="ghost" size="icon" onClick={() => removeGroup(gid)}>
                  <Trash2 className="h-4 w-4" />
                </Button>
              </div>

              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label className="text-xs">Agent</Label>
                  <Select
                    value={gc.agent_id ?? "__default__"}
                    onValueChange={(v) => updateGroup(gid, { agent_id: v === "__default__" ? undefined : v })}
                  >
                    <SelectTrigger className="text-base md:text-sm">
                      <SelectValue placeholder="Use channel default" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="__default__">Use channel default</SelectItem>
                      {agents.map((a) => (
                        <SelectItem key={a.agent_key} value={a.agent_key}>
                          {a.display_name || a.agent_key}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>

                <div className="flex items-center gap-2 pt-5">
                  <Switch
                    checked={gc.require_mention ?? false}
                    onCheckedChange={(v) => updateGroup(gid, { require_mention: v })}
                  />
                  <Label className="text-xs">Require @mention</Label>
                </div>
              </div>
            </CardContent>
          </Card>
        );
      })}

      {/* Add group */}
      <div className="flex items-center gap-2">
        <Input
          value={newGroupId}
          onChange={(e) => setNewGroupId(e.target.value)}
          placeholder="Group JID (e.g. 120363...@g.us)"
          className="text-base md:text-sm flex-1"
          onKeyDown={(e) => { if (e.key === "Enter") addGroup(); }}
        />
        <Button variant="outline" size="sm" onClick={() => addGroup()} disabled={!newGroupId.trim()}>
          <Plus className="h-4 w-4 mr-1" /> Add
        </Button>
      </div>

      {/* Quick-add known groups */}
      {unconfiguredGroups.length > 0 && (
        <div className="space-y-1">
          <p className="text-xs text-muted-foreground">Known groups:</p>
          <div className="flex flex-wrap gap-1">
            {unconfiguredGroups.map((g) => (
              <Button key={g.group_id} variant="outline" size="sm" className="text-xs" onClick={() => addGroup(g.group_id)}>
                <Plus className="h-3 w-3 mr-1" /> {g.group_id || g.group_id}
              </Button>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
