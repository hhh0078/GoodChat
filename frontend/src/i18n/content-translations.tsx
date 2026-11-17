/** 文件作用说明：为平台发布内容维护九种语言的名称说明，不自动改写已有业务原文。 */
"use client";
import { Form, Input, Tabs } from "antd";
import { languageNames, locales } from "./preferences";
import { useTranslator } from "./use-translator";

/** ContentTranslations 将选定语言译文写入表单 translations 字段。 */
export function ContentTranslations({
  description = true,
  nameLabel = "版本名称",
}: {
  description?: boolean;
  nameLabel?: string;
}) {
  const tr = useTranslator();
  return (
    <Tabs
      destroyOnHidden={false}
      items={locales.map((locale) => ({
        key: locale,
        label: languageNames[locale],
        forceRender: true,
        children: (
          <>
            <Form.Item name={["translations", locale, "name"]} label={tr(nameLabel)}>
              <Input maxLength={80} />
            </Form.Item>
            {description && (
              <Form.Item name={["translations", locale, "description"]} label={tr("说明")}>
                <Input.TextArea rows={3} maxLength={2000} />
              </Form.Item>
            )}
          </>
        ),
      }))}
    />
  );
}
