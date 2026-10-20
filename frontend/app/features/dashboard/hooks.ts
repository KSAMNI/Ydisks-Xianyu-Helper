import { useCallback,useEffect,useMemo,useRef,useState } from 'react';
import { getDateRange,getPreviousDateRange } from '../../../dateRange';
import { getDashboardStats,getItems,getOrderAnalytics,getValidOrders } from './api';
import { buildCategoryData,buildChartData,buildItemNameMap,buildProductSalesData,buildSourceData,getMaxProductSales,getRangeLabel,getTrendPercent,isCurrentDashboardRequest } from './state';
import type { DashboardData,DashboardRangeSelection,DashboardStatus } from './types';

/** Dashboard Hook 的输入参数。 */
export type UseDashboardOptions = DashboardRangeSelection;

/** Dashboard Hook 暴露给页面的可视化数据与操作。 */
export type UseDashboardResult = {
  /** data 表示返回数据。 */ data: DashboardData | null;
  /** status 表示状态。 */ status: DashboardStatus;
  /** chartData 表示图表数据。 */ chartData: ReturnType<typeof buildChartData>;
  /** productSalesData 表示商品销量数据。 */ productSalesData: ReturnType<typeof buildProductSalesData>;
  /** sourceData 表示来源统计数据。 */ sourceData: ReturnType<typeof buildSourceData>;
  /** categoryData 表示分类统计数据。 */ categoryData: ReturnType<typeof buildCategoryData>;
  /** maxProductSales 表示最大商品销量。 */ maxProductSales: number;
  /** trendPercent 表示趋势Percent。 */ trendPercent: string | null;
  /** selectedRangeLabel 表示选中范围Label。 */ selectedRangeLabel: string;
  /** dataRangeLabel 表示当前展示的经营数据实际所属的范围名称，范围切换未成功时仍是旧范围。 */ dataRangeLabel: string;
  /** rangeStale 表示当前展示的经营数据属于先前选择的范围，新范围尚未加载成功（加载中或失败）。 */ rangeStale: boolean;
  /** refresh 表示刷新函数。 */ refresh: () => void;
};

/** 生成时间范围选择的稳定键，用于判断已加载的经营数据是否仍属于当前选择。 */
const selectionKey = (selection: DashboardRangeSelection): string => `${selection.range}|${selection.customStartDate}|${selection.customEndDate}`;

/** 判断请求错误是否来自用户主动取消。 */
const isAbortError = (error: unknown): boolean => error instanceof Error && error.message === '请求已取消';

/** 统一转换 Dashboard 请求错误。 */
const errorMessage = (error: unknown, fallback: string): string => error instanceof Error ? error.message : fallback;

/** 加载 Dashboard 概览、趋势和订单明细，并隔离过期响应。 */
export const useDashboard = (options: UseDashboardOptions): UseDashboardResult => {
  // { 解构得到当前 Hook 返回的状态和操作函数。
  const { range, customStartDate, customEndDate } = options;
  // [overview, 解构得到当前 Hook 返回的状态和操作函数。
  const [overview, setOverview] = useState<Pick<DashboardData, 'stats' | 'items' | 'itemNames'> | null>(null);
  // [rangeData, 解构得到当前 Hook 返回的状态和操作函数。
  // rangeData 同时记录这批数据对应的范围选择键 key，范围切换后据此识别旧数据。
  const [rangeData, setRangeData] = useState<(Pick<DashboardData, 'analytics' | 'previousAnalytics' | 'validOrders' | 'dateRange'> & { /** key 是该批数据对应的范围选择键。 */ key: string; /** selection 是该批数据对应的范围选择，用于生成准确的范围标题。 */ selection: DashboardRangeSelection }) | null>(null);
  // [status, 解构得到当前 Hook 返回的状态和操作函数。
  const [status, setStatus] = useState<DashboardStatus>({ overview: 'idle', range: 'idle', error: '' });
  // [refreshKey, 解构得到当前 Hook 返回的状态和操作函数。
  const [refreshKey, setRefreshKey] = useState(0);
  // requestSequence 请求请求序号，负责当前功能中的对应处理。
  const requestSequence = useRef(0);
  // overviewSequence 隔离刷新后仍然完成的旧概览响应，避免忽略取消信号的底层实现覆盖新数据。
  const overviewSequence = useRef(0);
  // rangeSelection 范围选择，负责当前功能中的对应处理。
  const rangeSelection = useMemo(
    // selection 是当前时间范围的不可变快照。
    () => ({ range, customStartDate, customEndDate }),
    [customEndDate, customStartDate, range],
  );

  // refresh 刷新当前数据。
  const refresh = useCallback(
    // 重新加载按钮回调只递增版本号，实际请求由两个 effect 统一管理。
    () => setRefreshKey(/* 当前回调处理用户交互或异步状态变化。 */ value => value + 1),
    [],
  );

  useEffect(/* 当前回调同步 React 副作用和资源生命周期。 */ () => {
    // sequence 标识本轮概览请求，刷新或卸载后旧请求不得写入状态。
    const sequence = ++overviewSequence.current;
    // controller 请求取消控制器。
    const controller = new AbortController();
    setStatus(/* 当前回调处理用户交互或异步状态变化。 */ current => ({ ...current, overview: 'loading', error: '' }));
    Promise.all([
      getDashboardStats({ signal: controller.signal }),
      getItems(undefined, { signal: controller.signal }),
    ]).then(/* 当前回调处理用户交互或异步状态变化。 */ ([stats, items]) => {
      if (!isCurrentDashboardRequest(overviewSequence.current, sequence, controller.signal)) return;
      setOverview({ stats, items, itemNames: buildItemNameMap(items) });
      setStatus(/* 当前回调处理用户交互或异步状态变化。 */ current => ({ ...current, overview: 'success' }));
    }).catch(/* 当前回调处理用户交互或异步状态变化。 */ error => {
      if (!isCurrentDashboardRequest(overviewSequence.current, sequence, controller.signal) || isAbortError(error)) return;
      setStatus(/* 当前回调处理用户交互或异步状态变化。 */ current => ({ ...current, overview: 'error', error: errorMessage(error, '概览加载失败') }));
    });
    return /* 当前回调处理用户交互或异步状态变化。 */ () => controller.abort();
  }, [refreshKey]);

  useEffect(/* 当前回调同步 React 副作用和资源生命周期。 */ () => {
    // 自定义范围的日期未填齐时不请求，避免静默回退成最近 7 天而与选中状态不一致。
    if (range === 'custom' && (!customStartDate || !customEndDate)) return;
    // dateRange 日期范围，负责当前功能中的对应处理。
    let dateRange;
    try {
      dateRange = getDateRange(range, new Date(), customStartDate, customEndDate);
    } catch (/* error 保存经营数据请求失败原因；取消请求和过期代次均被忽略。 */ error) {
      setStatus(/* 当前回调处理用户交互或异步状态变化。 */ current => ({ ...current, range: 'error', error: errorMessage(error, '日期范围无效') }));
      return;
    }
    // previousRange 上一项范围，负责当前功能中的对应处理。
    const previousRange = getPreviousDateRange(dateRange);
    // sequence 请求序号。
    const sequence = ++requestSequence.current;
    // controller 请求取消控制器。
    const controller = new AbortController();
    setStatus(/* 当前回调处理用户交互或异步状态变化。 */ current => ({ ...current, range: 'loading', error: '' }));
    Promise.all([
      getOrderAnalytics({ start_date: dateRange.startDate, end_date: dateRange.endDate }, { signal: controller.signal }),
      getOrderAnalytics({ start_date: previousRange.startDate, end_date: previousRange.endDate }, { signal: controller.signal }),
      getValidOrders({ start_date: dateRange.startDate, end_date: dateRange.endDate }, { signal: controller.signal }),
    ]).then(/* 当前回调处理用户交互或异步状态变化。 */ ([analytics, previousAnalytics, validOrders]) => {
      if (!isCurrentDashboardRequest(requestSequence.current, sequence, controller.signal)) return;
      setRangeData({ analytics, previousAnalytics, validOrders, dateRange, key: selectionKey({ range, customStartDate, customEndDate }), selection: { range, customStartDate, customEndDate } });
      setStatus(/* 当前回调处理用户交互或异步状态变化。 */ current => ({ ...current, range: 'success' }));
    }).catch(/* 当前回调处理用户交互或异步状态变化。 */ error => {
      if (!isCurrentDashboardRequest(requestSequence.current, sequence, controller.signal) || isAbortError(error)) return;
      setStatus(/* 当前回调处理用户交互或异步状态变化。 */ current => ({ ...current, range: 'error', error: errorMessage(error, '经营数据加载失败') }));
    });
    return /* 当前回调处理用户交互或异步状态变化。 */ () => controller.abort();
  }, [customEndDate, customStartDate, range, refreshKey]);

  // data 数据。
  const data = useMemo<DashboardData | null>(
    // dashboardData 只有在概览和时间范围请求都成功后才对页面可见。
    () => {
      if (!overview || !rangeData) return null;
      // key 仅用于范围归属判断，不进入页面数据模型。
      const { key: _key, selection: _selection, ...visibleRange } = rangeData;
      return { ...overview, ...visibleRange };
    },
    [overview, rangeData],
  );
  // itemNames 商品Names，负责当前功能中的对应处理。
  const itemNames = overview?.itemNames || {};
  // analytics 统计分析数据。
  const analytics = rangeData?.analytics || null;
  // chartData 图表数据，负责当前功能中的对应处理。
  const chartData = useMemo(/* 当前回调计算并缓存派生数据。 */ () => analytics ? buildChartData(analytics) : [], [analytics]);
  // productSalesData productSales数据，负责当前功能中的对应处理。
  const productSalesData = useMemo(/* 当前回调计算并缓存派生数据。 */ () => analytics ? buildProductSalesData(analytics, itemNames) : [], [analytics, itemNames]);
  // colors 颜色列表。
  const colors = useMemo(
    // colors 是图表使用的主题颜色序列。
    () => ['rgb(var(--color-brand))', 'rgb(var(--color-brand-highlight))', 'rgb(var(--color-success-500))', 'rgb(var(--color-warning-500))', 'rgb(var(--color-accent-500))'],
    [],
  );
  // sourceData 来源数据，负责当前功能中的对应处理。
  const sourceData = useMemo(/* 当前回调计算并缓存派生数据。 */ () => analytics ? buildSourceData(analytics, itemNames, colors) : [], [analytics, colors, itemNames]);
  // categoryData 分类数据，负责当前功能中的对应处理。
  const categoryData = useMemo(/* 当前回调计算并缓存派生数据。 */ () => analytics ? buildCategoryData(analytics, itemNames, colors) : [], [analytics, colors, itemNames]);
  // maxProductSales 最大值ProductSales，负责当前功能中的对应处理。
  const maxProductSales = useMemo(/* 当前回调计算并缓存派生数据。 */ () => getMaxProductSales(productSalesData), [productSalesData]);

  return {
    data,
    status,
    chartData,
    productSalesData,
    sourceData,
    categoryData,
    maxProductSales,
    trendPercent: getTrendPercent(analytics, data?.previousAnalytics || null),
    selectedRangeLabel: getRangeLabel(rangeSelection),
    dataRangeLabel: getRangeLabel(rangeData?.selection || rangeSelection),
    rangeStale: rangeData !== null && rangeData.key !== selectionKey(rangeSelection),
    refresh,
  };
};
