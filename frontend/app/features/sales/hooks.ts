import { useCallback,useEffect,useRef,useState } from 'react';
import { getDateRange } from '../../../dateRange';
import { getSalesStats, type SalesStatsModel } from './api';

/** useSalesStats 的输入参数，描述当前选中的时间范围。 */
export type UseSalesStatsOptions = {
  /** range 是预设时间范围或自定义。 */
  range: 'today' | 'yesterday' | '3days' | '7days' | '30days' | 'custom';
  /** customStartDate 是自定义范围的起始日期。 */
  customStartDate: string;
  /** customEndDate 是自定义范围的结束日期。 */
  customEndDate: string;
  /** customRangeVersion 用于在自定义日期确认后触发重新加载。 */
  customRangeVersion: number;
};

/** useSalesStats 暴露给页面的可视化数据与操作。 */
export type UseSalesStatsResult = {
  /** data 是销售统计结果。 */
  data: SalesStatsModel | null;
  /** loading 表示是否正在加载。 */
  loading: boolean;
  /** error 是加载失败原因。 */
  error: string;
  /** refresh 是手动刷新函数。 */
  refresh: () => void;
};

/** 判断请求错误是否来自用户主动取消。 */
const isAbortError = (error: unknown): boolean => error instanceof Error && error.message === '请求已取消';

/** 统一转换销售统计请求错误。 */
const errorMessage = (error: unknown, fallback: string): string => error instanceof Error ? error.message : fallback;

/** 加载销售统计数据，并隔离过期响应。 */
export const useSalesStats = (options: UseSalesStatsOptions): UseSalesStatsResult => {
  // { 解构得到当前 Hook 的输入参数。
  const { range, customStartDate, customEndDate, customRangeVersion } = options;
  // [data, setData] 保存销售统计结果。
  const [data, setData] = useState<SalesStatsModel | null>(null);
  // [loading, setLoading] 保存加载状态。
  const [loading, setLoading] = useState(false);
  // [error, setError] 保存加载失败原因。
  const [error, setError] = useState('');
  // [refreshKey, setRefreshKey] 保存刷新版本号。
  const [refreshKey, setRefreshKey] = useState(0);
  // requestSequence 隔离刷新或范围切换后仍然完成的旧响应。
  const requestSequence = useRef(0);

  // refresh 触发重新加载。
  const refresh = useCallback(
    () => setRefreshKey(/* 当前回调处理用户交互或异步状态变化。 */ value => value + 1),
    [],
  );

  useEffect(/* effect 在时间范围或刷新版本变化时加载销售统计，并在卸载或过期时中止请求。 */ () => {
    // dateRange 是按用户本地时间解析后的日期范围。
    let dateRange;
    try {
      dateRange = getDateRange(range, new Date(), customStartDate, customEndDate);
    } catch (/* error 保存日期范围解析失败原因。 */ error) {
      setError(errorMessage(error, '日期范围无效'));
      setData(null);
      setLoading(false);
      return;
    }
    // sequence 标识本轮请求，过期响应不得写入状态。
    const sequence = ++requestSequence.current;
    // controller 请求取消控制器。
    const controller = new AbortController();
    setLoading(true);
    setError('');
    getSalesStats({ start_date: dateRange.startDate, end_date: dateRange.endDate }, { signal: controller.signal })
      .then(/* result 是服务端返回的销售统计结果。 */ result => {
        if (requestSequence.current !== sequence || controller.signal.aborted) return;
        setData(result);
        setLoading(false);
      })
      .catch(/* requestError 是销售统计加载失败原因。 */ requestError => {
        if (requestSequence.current !== sequence || controller.signal.aborted || isAbortError(requestError)) return;
        setError(errorMessage(requestError, '销售统计加载失败'));
        setData(null);
        setLoading(false);
      });
    return /* cleanup 在组件卸载或范围切换时释放请求。 */ () => controller.abort();
  }, [range, customStartDate, customEndDate, customRangeVersion, refreshKey]);

  return { data, loading, error, refresh };
};
