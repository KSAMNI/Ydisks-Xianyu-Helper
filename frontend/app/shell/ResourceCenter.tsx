import { useState, type ReactNode } from 'react';
import { PageHeader } from '../../shared/ui/PageHeader';

/** resourceSections 是统一入口下的业务分类；商品图片与消息图片仍保留各自的引用和删除保护。 */
const resourceSections = [
  { id: 'templates', label: '商品模板' },
  { id: 'images', label: '商品图片' },
  { id: 'messages', label: '消息图片' },
] as const;

/** ResourceSection 表示资源中心当前激活的分类。 */
export type ResourceSection = typeof resourceSections[number]['id'];

/** ResourceCenterProps 由应用壳注入跨 feature 页面，不让两个资源 feature 相互依赖。 */
interface ResourceCenterProps {
  /** initialSection 保留旧链接语义：library 打开模板，image-library 打开消息图片。 */
  initialSection: ResourceSection;
  /** templates 是原商品模板管理与批量铺货页面。 */
  templates: ReactNode;
  /** images 是商品主图、规格图的图片管理页面。 */
  images: ReactNode;
  /** messages 是回复、卡密和发货模板共用的消息图片页面。 */
  messages: ReactNode;
}

/** ResourceCenter 统一导航与分类，只挂载当前分类，切换时由子页面取消在途请求并丢弃未保存草稿。 */
export function ResourceCenter({ initialSection, templates, images, messages }: ResourceCenterProps) {
  // section 是用户选择的分类；setSection 仅响应页签点击，不复制子页面的服务端数据。
  const [section, setSection] = useState<ResourceSection>(initialSection);
  return <div className="space-y-6">
    <PageHeader title="资源中心" description="统一管理商品模板与图片素材。商品图片用于发布商品，消息图片用于自动回复、卡密和发货模板。" />
    <div className="flex flex-wrap gap-2" role="tablist" aria-label="资源分类">
      {resourceSections.map(/* entry 是一个业务分类，点击后卸载旧分类并挂载新分类。 */ entry => <button
        key={entry.id} type="button" role="tab" id={`resource-tab-${entry.id}`} aria-controls="resource-panel"
        aria-selected={section === entry.id} onClick={/* 用户切换分类时丢弃原页面临时编辑状态。 */ () => setSection(entry.id)}
        className={`rounded-xl px-4 py-2.5 text-sm font-bold ${section === entry.id ? 'bg-gray-950 text-white' : 'ios-btn-secondary'}`}
      >{entry.label}</button>)}
    </div>
    <div role="tabpanel" id="resource-panel" aria-labelledby={`resource-tab-${section}`}>
      {section === 'templates' ? templates : section === 'images' ? images : messages}
    </div>
  </div>;
}
