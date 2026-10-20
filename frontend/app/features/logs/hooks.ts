import { useCallback,useEffect,useRef,useState } from 'react';
import { fetchRuntimeLogs } from './api';
import { mergeRuntimeLogEntries, runtimeLogQuerySignature } from './models';
import type { RuntimeLogEntry, RuntimeLogQuery } from './types';

/** DEFAULT_RUNTIME_LOG_POLL_INTERVAL_MS 是页面默认的增量轮询间隔毫秒数。 */
export const DEFAULT_RUNTIME_LOG_POLL_INTERVAL_MS = 2000;

/** MIN_RUNTIME_LOG_POLL_INTERVAL_MS 是允许的最小轮询间隔，避免配置过密压垮服务端。 */
export const MIN_RUNTIME_LOG_POLL_INTERVAL_MS = 500;

/** DEFAULT_RUNTIME_LOG_PAGE_SIZE 是单次增量读取的条数上限。 */
export const DEFAULT_RUNTIME_LOG_PAGE_SIZE = 200;

/** EMPTY_RUNTIME_LOG_QUERY 是页面初始过滤条件，表示展示全部来源与等级。 */
export const EMPTY_RUNTIME_LOG_QUERY: RuntimeLogQuery = { level: '', source: '', keyword: '' };

/** UseRuntimeLogsOptions 是运行日志 Hook 的可选配置。 */
export type UseRuntimeLogsOptions = {
  /** enabled 表示页面是否处于可轮询状态；为假时停止轮询但保留已加载日志。 */
  enabled?: boolean;
  /** pollIntervalMs 是自动增量轮询间隔毫秒数，低于最小间隔时按最小间隔执行。 */
  pollIntervalMs?: number;
  /** pageSize 是单次增量读取条数上限。 */
  pageSize?: number;
};

/** UseRuntimeLogsResult 是运行日志 Hook 暴露给页面的状态与操作。 */
export type UseRuntimeLogsResult = {
  /** entries 是当前已加载的日志流，按序号升序排列。 */
  entries: RuntimeLogEntry[];
  /** query 是当前过滤条件。 */
  query: RuntimeLogQuery;
  /** paused 表示是否已暂停自动轮询。 */
  paused: boolean;
  /** loading 表示当前是否有正在进行的增量请求。 */
  loading: boolean;
  /** error 是最近一次加载失败原因；空字符串表示没有错误。 */
  error: string;
  /** dropped 是服务端已覆盖的累计记录数，用于提示历史记录不完整。 */
  dropped: number;
  /** capacity 是服务端可保留的最大记录数；零表示服务端未启用日志缓冲。 */
  capacity: number;
  /** setQuery 局部更新过滤条件；条件变化会重置游标并清空已加载日志。 */
  setQuery: (patch: Partial<RuntimeLogQuery>) => void;
  /** setPaused 设置暂停状态；恢复后从当前游标继续，不会丢失中间记录。 */
  setPaused: (next: boolean) => void;
  /** clear 清空已展示日志但保留游标，避免清屏后重复拉取旧记录。 */
  clear: () => void;
  /** reload 从零游标重新加载当前过滤条件下的日志，并恢复自动轮询。 */
  reload: () => void;
};

/** isRuntimeLogAbortError 判断请求错误是否来自调用方主动取消。 */
const isRuntimeLogAbortError = (error: unknown): boolean => error instanceof Error && error.message === '请求已取消';

/** runtimeLogErrorMessage 统一转换运行日志请求错误为用户可见文本。 */
const runtimeLogErrorMessage = (error: unknown, fallback: string): string => (error instanceof Error ? error.message : fallback);

/** useRuntimeLogs 维护运行日志增量轮询、过滤、暂停和清屏状态。 */
export const useRuntimeLogs = (options: UseRuntimeLogsOptions = {}): UseRuntimeLogsResult => {
  // { 解构得到当前 Hook 的可选配置，未提供时使用默认轮询参数。
  const { enabled = true, pollIntervalMs = DEFAULT_RUNTIME_LOG_POLL_INTERVAL_MS, pageSize = DEFAULT_RUNTIME_LOG_PAGE_SIZE } = options;
  // [entries, setEntries] 保存已加载的日志流。
  const [entries, setEntries] = useState<RuntimeLogEntry[]>([]);
  // [query, setQueryState] 保存当前过滤条件。
  const [query, setQueryState] = useState<RuntimeLogQuery>(EMPTY_RUNTIME_LOG_QUERY);
  // [paused, setPausedState] 保存用户暂停状态。
  const [paused, setPausedState] = useState(false);
  // [loading, setLoading] 保存增量请求进行状态。
  const [loading, setLoading] = useState(false);
  // [error, setError] 保存最近一次加载失败原因。
  const [error, setError] = useState('');
  // [stats, setStats] 保存服务端缓冲区边界，用于生成保留提示。
  const [stats, setStats] = useState({ dropped: 0, capacity: 0 });
  // [reloadKey, setReloadKey] 保存手动重新加载版本号。
  const [reloadKey, setReloadKey] = useState(0);
  // cursorRef 保存下一次增量读取使用的排他游标，避免轮询期间触发额外渲染。
  const cursorRef = useRef(0);
  // sequenceRef 保存当前日志流代次，过期响应不得写入状态。
  const sequenceRef = useRef(0);
  // querySignature 是过滤条件签名，条件变化时必须重置游标与已加载日志。
  const querySignature = runtimeLogQuerySignature(query);

  useEffect(/* resetEffect 在过滤条件或重新加载版本变化时重置游标与已加载日志。 */ () => {
    cursorRef.current = 0;
    setEntries([]);
    setError('');
    setStats({ dropped: 0, capacity: 0 });
  }, [querySignature, reloadKey]);

  useEffect(/* pollingEffect 在启用且未暂停时立即读取一次增量，并按间隔持续轮询。 */ () => {
    if (!enabled || paused) return;
    // sequence 标识本轮日志流，过期响应不得写入状态。
    const sequence = ++sequenceRef.current;
    // controller 是当前日志流的请求取消控制器。
    const controller = new AbortController();
    // cancelled 表示当前日志流是否已被清理函数结束。
    let cancelled = false;
    // inFlight 表示本日志流是否已有未返回的增量请求；同一游标的请求不得并发，避免乱序响应让游标倒退。
    let inFlight = false;
    // loadIncrement 读取一次增量日志并按代次守卫写入状态；上一轮未返回时跳过本轮。
    const loadIncrement = async (): Promise<void> => {
      if (inFlight) return;
      inFlight = true;
      setLoading(true);
      try {
        // page 是本次增量查询返回的日志页。
        const page = await fetchRuntimeLogs(query, cursorRef.current, pageSize, { signal: controller.signal });
        if (cancelled || sequenceRef.current !== sequence || controller.signal.aborted) return;
        cursorRef.current = page.nextCursor;
        setEntries(/* previous 是变更前已加载的日志流。 */ previous => mergeRuntimeLogEntries(previous, page.entries, page.capacity));
        setStats({ dropped: page.dropped, capacity: page.capacity });
        setError('');
      } catch (requestError /* requestError 是本次增量查询失败原因。 */) {
        if (cancelled || sequenceRef.current !== sequence || isRuntimeLogAbortError(requestError)) return;
        setError(runtimeLogErrorMessage(requestError, '运行日志加载失败'));
      } finally {
        inFlight = false;
        if (!cancelled && sequenceRef.current === sequence) setLoading(false);
      }
    };
    void loadIncrement();
    // timer 是按配置间隔触发增量读取的定时器。
    const timer = window.setInterval(/* intervalCallback 按轮询间隔触发一次增量读取。 */ () => { void loadIncrement(); }, Math.max(MIN_RUNTIME_LOG_POLL_INTERVAL_MS, pollIntervalMs));
    return /* pollingCleanup 在暂停、配置变化或卸载时停止定时器、取消在途请求并复位加载状态。 */ () => {
      cancelled = true;
      window.clearInterval(timer);
      controller.abort();
      // 取消在途请求后必须复位加载状态，否则暂停期间页面会一直显示"加载中"。
      if (sequenceRef.current === sequence) setLoading(false);
    };
  }, [enabled, paused, pollIntervalMs, pageSize, query, reloadKey]);

  // setQuery 合并过滤条件补丁，具体重置行为由条件签名 effect 负责。
  const setQuery = useCallback(
    /* patch 是需要覆盖的过滤条件字段。 */ (patch: Partial<RuntimeLogQuery>) => setQueryState(/* previous 是变更前的过滤条件。 */ previous => ({ ...previous, ...patch })),
    [],
  );

  // setPaused 设置暂停状态，恢复后从当前游标继续读取。
  const setPaused = useCallback(/* next 是目标暂停状态。 */ (next: boolean) => setPausedState(next), []);

  // clear 清空已展示日志但保留游标，避免清屏后重复拉取旧记录。
  const clear = useCallback(/* 清屏操作不接收参数。 */ () => setEntries([]), []);

  // reload 提升重新加载版本号，并恢复轮询，保证暂停状态下点击刷新也能真正读取日志。
  const reload = useCallback(/* 重新加载操作不接收参数。 */ () => {
    setPausedState(false);
    setReloadKey(/* value 是变更前的重新加载版本号。 */ value => value + 1);
  }, []);

  return {
    entries,
    query,
    paused,
    loading,
    error,
    dropped: stats.dropped,
    capacity: stats.capacity,
    setQuery,
    setPaused,
    clear,
    reload,
  };
};
