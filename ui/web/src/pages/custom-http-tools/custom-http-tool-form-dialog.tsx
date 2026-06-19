import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { CustomHTTPToolInput } from "./hooks/use-custom-http-tools";

interface CustomHTTPToolFormDialogProps {
  open: boolean;
  onClose: () => void;
  onSubmit: (data: CustomHTTPToolInput) => Promise<void>;
}

const HTTP_METHODS = ["POST", "GET", "PUT", "PATCH", "DELETE"];

const DEFAULT_PARAMETERS = JSON.stringify(
  { type: "object", properties: {}, required: [] },
  null,
  2,
);

export function CustomHTTPToolFormDialog({ open, onClose, onSubmit }: CustomHTTPToolFormDialogProps) {
  const { t } = useTranslation("customHTTPTools");
  const [submitting, setSubmitting] = useState(false);

  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [url, setUrl] = useState("");
  const [method, setMethod] = useState("POST");
  const [headersRaw, setHeadersRaw] = useState("");
  const [timeout, setTimeout_] = useState("30");
  const [parametersRaw, setParametersRaw] = useState(DEFAULT_PARAMETERS);
  const [agentId, setAgentId] = useState("");
  const [errors, setErrors] = useState<Record<string, string>>({});

  function validate(): boolean {
    const errs: Record<string, string> = {};
    if (!name.trim()) errs.name = t("form.errors.nameRequired");
    if (!url.trim()) errs.url = t("form.errors.urlRequired");
    if (headersRaw.trim()) {
      try { JSON.parse(headersRaw); } catch { errs.headers = t("form.errors.invalidJson"); }
    }
    try { JSON.parse(parametersRaw); } catch { errs.parameters = t("form.errors.invalidJson"); }
    setErrors(errs);
    return Object.keys(errs).length === 0;
  }

  async function handleSubmit() {
    if (!validate()) return;
    setSubmitting(true);
    try {
      const headers = headersRaw.trim() ? JSON.parse(headersRaw) : undefined;
      const parameters = JSON.parse(parametersRaw);
      await onSubmit({
        name: name.trim(),
        description: description.trim(),
        url: url.trim(),
        parameters,
        config: {
          url: url.trim(),
          method,
          headers,
          timeout: parseInt(timeout, 10) || 30,
        },
        agent_id: agentId.trim() || null,
      });
      handleClose();
    } finally {
      setSubmitting(false);
    }
  }

  function handleClose() {
    setName("");
    setDescription("");
    setUrl("");
    setMethod("POST");
    setHeadersRaw("");
    setTimeout_("30");
    setParametersRaw(DEFAULT_PARAMETERS);
    setAgentId("");
    setErrors({});
    onClose();
  }

  return (
    <Dialog open={open} onOpenChange={(v) => !v && handleClose()}>
      <DialogContent className="max-w-lg max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{t("form.title")}</DialogTitle>
        </DialogHeader>

        <div className="grid gap-4 py-2">
          <div className="grid gap-1.5">
            <Label htmlFor="cht-name">{t("form.name")} *</Label>
            <Input
              id="cht-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t("form.namePlaceholder")}
            />
            {errors.name && <p className="text-xs text-destructive">{errors.name}</p>}
          </div>

          <div className="grid gap-1.5">
            <Label htmlFor="cht-description">{t("form.description")}</Label>
            <Textarea
              id="cht-description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t("form.descriptionPlaceholder")}
              rows={2}
            />
          </div>

          <div className="grid gap-1.5">
            <Label htmlFor="cht-url">{t("form.url")} *</Label>
            <Input
              id="cht-url"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder="https://example.com/tool-endpoint"
            />
            {errors.url && <p className="text-xs text-destructive">{errors.url}</p>}
          </div>

          <div className="grid gap-1.5">
            <Label htmlFor="cht-method">{t("form.method")}</Label>
            <Select value={method} onValueChange={setMethod}>
              <SelectTrigger id="cht-method">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {HTTP_METHODS.map((m) => (
                  <SelectItem key={m} value={m}>{m}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="grid gap-1.5">
            <Label htmlFor="cht-timeout">{t("form.timeout")}</Label>
            <Input
              id="cht-timeout"
              type="number"
              min={1}
              max={300}
              value={timeout}
              onChange={(e) => setTimeout_(e.target.value)}
            />
          </div>

          <div className="grid gap-1.5">
            <Label htmlFor="cht-headers">{t("form.headers")}</Label>
            <Textarea
              id="cht-headers"
              value={headersRaw}
              onChange={(e) => setHeadersRaw(e.target.value)}
              placeholder={'{\n  "Authorization": "Bearer ..."\n}'}
              rows={3}
              className="font-mono text-xs"
            />
            {errors.headers && <p className="text-xs text-destructive">{errors.headers}</p>}
          </div>

          <div className="grid gap-1.5">
            <Label htmlFor="cht-parameters">{t("form.parameters")}</Label>
            <Textarea
              id="cht-parameters"
              value={parametersRaw}
              onChange={(e) => setParametersRaw(e.target.value)}
              rows={6}
              className="font-mono text-xs"
            />
            {errors.parameters && <p className="text-xs text-destructive">{errors.parameters}</p>}
          </div>

          <div className="grid gap-1.5">
            <Label htmlFor="cht-agent-id">{t("form.agentId")}</Label>
            <Input
              id="cht-agent-id"
              value={agentId}
              onChange={(e) => setAgentId(e.target.value)}
              placeholder={t("form.agentIdPlaceholder")}
            />
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={handleClose} disabled={submitting}>
            {t("form.cancel")}
          </Button>
          <Button onClick={handleSubmit} disabled={submitting}>
            {submitting ? t("form.submitting") : t("form.submit")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
