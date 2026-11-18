/** 文件作用说明：提供逐字段语言时区检测确认、URL 直用、持久偏好与九种语言切换入口。 */
"use client";
import { GlobalOutlined } from "@ant-design/icons";
import { Button, ConfigProvider, Modal, Select, Space, Typography } from "antd";
import { NextIntlClientProvider } from "next-intl";
import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { antdLocales } from "./antd-locales";
import {
  availableTimezones,
  languageNames,
  locales,
  normalizeLocale,
  parseSavedPreferences,
  resolvePreferences,
  validTimezone,
  type Preferences,
  type SavedPreferences,
} from "./preferences";
import { createTextTranslator, setBrowserPreferences, type Messages } from "./runtime";

const LocaleContext = createContext<Preferences>({ locale: "en", timezone: "UTC" });
const storageKey = "goodchat.preferences.v1";
const sessionKey = "goodchat.url-preferences.v1";

/** readSaved 只读取经过验证的语言时区，禁用存储时仍允许当前窗口操作。 */
function readSaved(kind: "localStorage" | "sessionStorage", key: string): SavedPreferences {
  try {
    const value = JSON.parse(window[kind].getItem(key) || "{}");
    return parseSavedPreferences(value);
  } catch {
    return {};
  }
}

/** persistCookies 为后续服务端渲染同步有效偏好，不写入账号或租户业务设置。 */
function persistCookies(value: Preferences): void {
  const secure = location.protocol === "https:" ? "; Secure" : "";
  document.cookie = `goodchat.locale=${value.locale}; Path=/; Max-Age=31536000; SameSite=Lax${secure}`;
  document.cookie = `goodchat.timezone=${value.timezone}; Path=/; Max-Age=31536000; SameSite=Lax${secure}`;
  document.documentElement.lang = value.locale;
}

/** LocaleProvider 在浏览器获取电脑偏好，URL 有效字段免确认，其他未确认字段集中确认一次。 */
export function LocaleProvider({
  children,
  initial,
  messages: initialMessages,
}: {
  children: ReactNode;
  initial: Preferences;
  messages: Messages;
}) {
  const [preferences, setPreferences] = useState(initial);
  const [messages, setMessages] = useState(initialMessages);
  const [draft, setDraft] = useState(initial);
  const [pending, setPending] = useState({ locale: false, timezone: false });
  const [open, setOpen] = useState(false);
  const [embeddedChat, setEmbeddedChat] = useState(false);
  const [manual, setManual] = useState(false);
  const [busy, setBusy] = useState(false);
  const revision = useRef(0);
  const initialized = useRef(false);
  const authenticated = useRef(false);
  const tr = createTextTranslator(preferences.locale, messages);
  setBrowserPreferences(preferences, messages);

  /** apply 加载单个语言包并更新当前环境，过期的异步切换不会覆盖新选择。 */
  async function apply(next: Preferences) {
    const version = ++revision.current;
    const catalog: Messages = (await import(`./messages/${next.locale}.json`)).default;
    if (version !== revision.current) return;
    setBrowserPreferences(next, catalog);
    setMessages(catalog);
    setPreferences(next);
    persistCookies(next);
  }

  useEffect(() => {
    if (initialized.current) return;
    initialized.current = true;
    const query = new URLSearchParams(location.search);
    setEmbeddedChat(location.pathname.startsWith("/c/") && query.get("embed") === "1");
    const pathLocale = normalizeLocale(location.pathname.split("/")[1]);
    if (!query.has("lang") && pathLocale) query.set("lang", pathLocale);
    let saved: SavedPreferences = {},
      session: SavedPreferences = {};
    try {
      saved = readSaved("localStorage", storageKey);
      session = readSaved("sessionStorage", sessionKey);
    } catch {
      /* 存储不可用时仍按当前 URL 与设备工作。 */
    }
    const urlValues: SavedPreferences = {};
    if (query.getAll("lang").length === 1) urlValues.locale = normalizeLocale(query.get("lang"));
    if (query.getAll("timezone").length === 1)
      urlValues.timezone = validTimezone(query.get("timezone"));
    const retained = {
      ...session,
      ...Object.fromEntries(Object.entries(urlValues).filter(([, value]) => value)),
    };
    try {
      sessionStorage.setItem(sessionKey, JSON.stringify(retained));
    } catch {
      /* 保留当前页偏好。 */
    }
    let deviceTimezone: string | undefined;
    try {
      deviceTimezone = Intl.DateTimeFormat().resolvedOptions().timeZone;
    } catch {
      /* 检测失败时由用户手选。 */
    }
    const resolved = resolvePreferences(
      query,
      { ...saved, ...retained },
      { languages: navigator.languages, timezone: deviceTimezone },
    );
    try {
      const lastDevice = localStorage.getItem("goodchat.device-timezone.v1");
      if (
        lastDevice &&
        deviceTimezone &&
        validTimezone(deviceTimezone) &&
        lastDevice !== deviceTimezone &&
        !urlValues.timezone &&
        !retained.timezone
      ) {
        resolved.timezone = deviceTimezone;
        resolved.confirmTimezone = true;
      }
    } catch {
      /* 禁用存储时沿用首次确认流程。 */
    }
    const next = { locale: resolved.locale, timezone: resolved.timezone };
    setDraft(next);
    setPending({ locale: resolved.confirmLocale, timezone: resolved.confirmTimezone });
    void apply(next).then(() => {
      if (resolved.confirmLocale || resolved.confirmTimezone) setOpen(true);
      else if (next.locale !== initial.locale || next.timezone !== initial.timezone)
        window.dispatchEvent(new Event("goodchat:preferences-changed"));
    });
  }, []); // 首次访问解析一次，站内导航复用已确认设置。

  useEffect(() => {
    if (!initialized.current) return;
    const query = new URLSearchParams(location.search);
    const locale =
      query.getAll("lang").length === 1
        ? normalizeLocale(query.get("lang"))
        : !query.has("lang")
          ? normalizeLocale(location.pathname.split("/")[1])
          : undefined;
    const timezone =
      query.getAll("timezone").length === 1 ? validTimezone(query.get("timezone")) : undefined;
    if (
      (locale && locale !== preferences.locale) ||
      (timezone && timezone !== preferences.timezone)
    )
      void apply({
        locale: locale || preferences.locale,
        timezone: timezone || preferences.timezone,
      });
  }, [initial.locale, initial.timezone]);

  useEffect(() => {
    /** syncConfirmed 只同步用户已确认的本机字段，URL 自动选择不写入已有账号。 */
    function syncConfirmed() {
      authenticated.current = true;
      const body = readSaved("localStorage", storageKey);
      if (body.locale || body.timezone)
        void fetch("/api/v1/auth/preferences", {
          method: "PATCH",
          credentials: "same-origin",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body),
        }).catch(() => {});
    }
    window.addEventListener("goodchat:authenticated", syncConfirmed);
    return () => window.removeEventListener("goodchat:authenticated", syncConfirmed);
  }, []);

  /** confirm 仅保存需要确认的字段；明确手动设置时同步个人偏好，不改租户时区。 */
  async function confirm() {
    setBusy(true);
    try {
      await apply(draft);
      let saved: SavedPreferences = {};
      try {
        saved = readSaved("localStorage", storageKey);
        if (manual || pending.locale) saved.locale = draft.locale;
        if (manual || pending.timezone) saved.timezone = draft.timezone;
        localStorage.setItem(storageKey, JSON.stringify(saved));
        localStorage.setItem(
          "goodchat.device-timezone.v1",
          Intl.DateTimeFormat().resolvedOptions().timeZone,
        );
        if (manual) sessionStorage.removeItem(sessionKey);
      } catch {
        /* 禁用存储不阻止当前窗口确认。 */
      }
      setOpen(false);
      let targetURL: string | undefined;
      if (manual) {
        const url = new URL(location.href);
        url.searchParams.set("lang", draft.locale);
        url.searchParams.set("timezone", draft.timezone);
        targetURL = url.pathname + url.search + url.hash;
      }
      // URL 自动应用不写账号；只有用户主动确认的字段才进入个人设置。
      const body: SavedPreferences = {};
      if (manual || pending.locale) body.locale = draft.locale;
      if (manual || pending.timezone) body.timezone = draft.timezone;
      if (authenticated.current)
        void fetch("/api/v1/auth/preferences", {
          method: "PATCH",
          credentials: "same-origin",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body),
        }).catch(() => {});
      if (targetURL) {
        // 网页由 Next 路由处理并取消默认操作；桌面端仅同步地址，不重新启动工作台。
        const changed = new CustomEvent("goodchat:preferences-changed", {
          detail: { url: targetURL },
          cancelable: true,
        });
        if (window.dispatchEvent(changed)) history.replaceState(history.state, "", targetURL);
      } else {
        window.dispatchEvent(new Event("goodchat:preferences-changed"));
      }
    } finally {
      setBusy(false);
    }
  }

  const zones = availableTimezones(draft.timezone);
  return (
    <NextIntlClientProvider
      locale={preferences.locale}
      messages={messages}
      timeZone={preferences.timezone}
    >
      <ConfigProvider
        locale={antdLocales[preferences.locale]}
        theme={{ token: { colorPrimary: "#1455ff" } }}
      >
        <LocaleContext.Provider value={preferences}>
          {children}
          {!embeddedChat && (
            <Button
              className="locale-settings-button"
              icon={<GlobalOutlined />}
              aria-label={tr("语言与时区")}
              onClick={() => {
                setDraft(preferences);
                setManual(true);
                setOpen(true);
              }}
            >
              {languageNames[preferences.locale]}
            </Button>
          )}
          <Modal
            open={open}
            title={tr(manual ? "语言与时区" : "确认你的语言与时区")}
            closable={manual}
            mask={{ closable: false }}
            keyboard={manual}
            onCancel={() => setOpen(false)}
            footer={
              <Button type="primary" loading={busy} onClick={() => void confirm()}>
                {tr("确认并继续")}
              </Button>
            }
          >
            <Typography.Paragraph>
              {tr("系统已读取设备设置，请确认后继续。URL 已指定的项目会直接采用。")}
            </Typography.Paragraph>
            <Space orientation="vertical" size={16} style={{ width: "100%" }}>
              <label htmlFor="goodchat-language">{tr("语言")}</label>
              <Select
                id="goodchat-language"
                style={{ width: "100%" }}
                disabled={!manual && !pending.locale}
                value={draft.locale}
                options={locales.map((locale) => ({ value: locale, label: languageNames[locale] }))}
                onChange={(locale) => setDraft((previous) => ({ ...previous, locale }))}
              />
              <label htmlFor="goodchat-timezone">{tr("时区")}</label>
              <Select
                id="goodchat-timezone"
                showSearch
                style={{ width: "100%" }}
                disabled={!manual && !pending.timezone}
                value={draft.timezone}
                options={zones.map((timezone) => ({
                  value: timezone,
                  label: timezone.replaceAll("_", " "),
                }))}
                onChange={(timezone) => setDraft((previous) => ({ ...previous, timezone }))}
              />
              <Typography.Text type="secondary">
                {tr("此设置控制个人时间显示。企业营业时间和统计仍使用企业时区。")}
              </Typography.Text>
            </Space>
          </Modal>
        </LocaleContext.Provider>
      </ConfigProvider>
    </NextIntlClientProvider>
  );
}

/** usePreferences 读取当前窗口已选定的个人语言与显示时区。 */
export function usePreferences(): Preferences {
  return useContext(LocaleContext);
}
