import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Globe, Plus, RefreshCw, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { SearchInput } from "@/components/shared/search-input";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { useCustomHTTPTools, type CustomHTTPToolData } from "./hooks/use-custom-http-tools";
import { CustomHTTPToolFormDialog } from "./custom-http-tool-form-dialog";

export function CustomHTTPToolsPage() {
  const { t } = useTranslation("customHTTPTools");
  const { tools, loading, fetching, refresh, createTool, deleteTool } = useCustomHTTPTools();

  const [search, setSearch] = useState("");
  const [formOpen, setFormOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<CustomHTTPToolData | null>(null);

  const spinning = useMinLoading(fetching, 600);
  const showSkeleton = useDeferredLoading(loading);

  const filtered = tools.filter(
    (t) =>
      !search ||
      t.name.toLowerCase().includes(search.toLowerCase()) ||
      t.description.toLowerCase().includes(search.toLowerCase()),
  );

  return (
    <div className="p-4 sm:p-6 pb-10">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <div className="flex gap-2">
            <Button size="sm" variant="outline" onClick={() => refresh()}>
              <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} />
            </Button>
            <Button size="sm" onClick={() => setFormOpen(true)}>
              <Plus className="h-3.5 w-3.5 mr-1" />
              {t("addTool")}
            </Button>
          </div>
        }
      />

      <div className="mt-4">
        <SearchInput
          value={search}
          onChange={setSearch}
          placeholder={t("searchPlaceholder")}
          className="max-w-sm"
        />
      </div>

      <div className="mt-4">
        {showSkeleton ? (
          <TableSkeleton rows={5} />
        ) : filtered.length === 0 ? (
          <EmptyState
            icon={Globe}
            title={search ? t("noMatchTitle") : t("emptyTitle")}
            description={search ? t("noMatchDescription") : t("emptyDescription")}
          />
        ) : (
          <div className="rounded-md border">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">{t("table.name")}</th>
                  <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">{t("table.url")}</th>
                  <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">{t("table.method")}</th>
                  <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">{t("table.scope")}</th>
                  <th className="px-4 py-2.5 text-right font-medium text-muted-foreground">{t("table.actions")}</th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((tool) => (
                  <tr key={tool.id} className="border-b last:border-0 hover:bg-muted/30">
                    <td className="px-4 py-3">
                      <div className="flex items-start gap-2">
                        <Globe className="h-4 w-4 text-muted-foreground shrink-0 mt-0.5" />
                        <div>
                          <p className="font-medium">{tool.name}</p>
                          {tool.description && (
                            <p className="text-xs text-muted-foreground mt-0.5">{tool.description}</p>
                          )}
                        </div>
                      </div>
                    </td>
                    <td className="px-4 py-3 font-mono text-xs text-muted-foreground max-w-[200px] truncate">
                      {tool.config.url}
                    </td>
                    <td className="px-4 py-3">
                      <Badge variant="secondary">{tool.config.method}</Badge>
                    </td>
                    <td className="px-4 py-3">
                      {tool.agent_id ? (
                        <span className="text-xs text-muted-foreground font-mono">{tool.agent_id}</span>
                      ) : (
                        <Badge variant="outline">{t("table.global")}</Badge>
                      )}
                    </td>
                    <td className="px-4 py-3 text-right">
                      <Button
                        size="icon"
                        variant="ghost"
                        className="h-7 w-7 text-muted-foreground hover:text-destructive"
                        onClick={() => setDeleteTarget(tool)}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      <CustomHTTPToolFormDialog
        open={formOpen}
        onClose={() => setFormOpen(false)}
        onSubmit={createTool}
      />

      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(v) => !v && setDeleteTarget(null)}
        onConfirm={async () => {
          if (deleteTarget) await deleteTool(deleteTarget.id);
          setDeleteTarget(null);
        }}
        title={t("deleteDialog.title")}
        description={t("deleteDialog.description", { name: deleteTarget?.name ?? "" })}
        confirmLabel={t("deleteDialog.confirm")}
        variant="destructive"
      />
    </div>
  );
}
