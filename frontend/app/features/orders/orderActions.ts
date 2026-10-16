import { useCallback,useEffect,useRef,useState,type Dispatch,type SetStateAction } from 'react';
import type { Order } from './api';
import { deleteOrder,manualShipOrder,orderSyncErrorMessage,syncOrders,syncSingleOrder,updateOrder } from './api';
import { formatOrderSyncResult,formatSingleSyncResult } from './syncResult';

// OrderShipMode 表示订单发货操作的两种业务模式。
export type OrderShipMode = 'status_only' | 'full_delivery';

// OrderShipResult 描述订单发货弹窗展示的结果信息。
export interface OrderShipResult {
  // success 表示发货操作是否成功。
  success: boolean;
  // message 保存发货操作的用户可见说明。
  message: string;
}

// OrderActionsOptions 描述订单动作协调器依赖的查询状态和页面操作。
export interface OrderActionsOptions {
  // orders 保存当前分页中的订单，用于删除后的分页判断。
  orders: Order[];
  // page 保存当前订单列表页码。
  page: number;
  // accountFilter 保存当前账号筛选条件。
  accountFilter: string;
  // filter 保存当前订单状态筛选条件。
  filter: string;
  // searchText 是当前搜索条件，用于隔离搜索切换前的删除结果。
  searchText?: string;
  // setPage 更新订单列表页码。
  setPage: Dispatch<SetStateAction<number>>;
  // loadOrders 刷新当前筛选条件下的订单列表。
  loadOrders: () => Promise<void>;
}

// OrderActionsState 暴露订单页面动作、弹窗状态和异步结果。
export interface OrderActionsState {
  // showDetailModal 表示订单详情弹窗是否打开。
  showDetailModal: boolean;
  // selectedOrder 保存当前查看详情的订单。
  selectedOrder: Order | null;
  // showEditModal 表示订单编辑弹窗是否打开。
  showEditModal: boolean;
  // editingOrder 保存当前编辑中的订单草稿。
  editingOrder: Partial<Order> | null;
  // showShipModal 表示订单发货弹窗是否打开。
  showShipModal: boolean;
  // shipOrderId 保存当前待发货订单号。
  shipOrderId: string;
  // shipLoading 表示发货请求是否正在执行。
  shipLoading: boolean;
  // shipResult 保存最近一次发货操作结果。
  shipResult: OrderShipResult | null;
  // syncingOrderId 保存当前正在单笔同步或强制获取的订单号。
  syncingOrderId: string | null;
  // syncNotice 保存单笔同步与强制获取的用户可见提示；等待账号刷新间隔时也会写入解释文案。
  syncNotice: string | null;
  // setSyncNotice 允许页面在用户确认后清除提示条，不改变正在执行的请求。
  setSyncNotice: Dispatch<SetStateAction<string | null>>;
  // deletingOrderId 保存当前正在删除的订单号。
  deletingOrderId: string | null;
  // handleSync 同步当前筛选条件下的订单。
  handleSync: () => Promise<void>;
  // handleShip 打开发货弹窗并选择待发货订单。
  handleShip: (orderId: string) => void;
  // executeShip 执行指定模式的订单发货。
  executeShip: (mode: OrderShipMode) => Promise<void>;
  // handleViewDetail 打开指定订单的详情弹窗。
  handleViewDetail: (order: Order) => void;
  // handleEdit 打开指定订单的编辑弹窗。
  handleEdit: (order: Order) => void;
  // handleSaveEdit 保存当前订单编辑草稿。
  handleSaveEdit: () => Promise<void>;
  // updateEditingOrder 更新当前订单编辑草稿的局部字段。
  updateEditingOrder: (patch: Partial<Order>) => void;
  // handleSyncSingle 同步指定的单笔订单；forced 为真时按用户确认跳过本地账号刷新间隔。
  handleSyncSingle: (orderId: string, forced?: boolean) => Promise<void>;
  // handleForceFetchDetail 在用户确认后强制获取指定订单的最新平台详情。
  handleForceFetchDetail: (orderId: string) => Promise<void>;
  // handleDelete 删除指定订单并处理分页回退。
  handleDelete: (orderId: string) => Promise<void>;
  // closeDetailModal 关闭订单详情弹窗。
  closeDetailModal: () => void;
  // closeEditModal 关闭订单编辑弹窗。
  closeEditModal: () => void;
  // closeShipModal 关闭订单发货弹窗并清理结果。
  closeShipModal: () => void;
}

// orderErrorMessage 将未知异常转换为稳定的订单动作提示；统一错误码的语义由 feature API adapter 解释。
const orderErrorMessage = (error: unknown, fallback: string): string => orderSyncErrorMessage(error, fallback);

// useOrderActions 集中管理订单同步、发货、编辑、删除和弹窗生命周期。
export const useOrderActions = ({ orders, page, accountFilter, filter, searchText = '', setPage, loadOrders }: OrderActionsOptions): OrderActionsState => {
  // showDetailModal 表示订单详情弹窗是否打开。
  const [showDetailModal, setShowDetailModal] = useState(false);
  // selectedOrder 保存当前查看详情的订单。
  const [selectedOrder, setSelectedOrder] = useState<Order | null>(null);
  // showEditModal 表示订单编辑弹窗是否打开。
  const [showEditModal, setShowEditModal] = useState(false);
  // editingOrder 保存当前编辑中的订单草稿。
  const [editingOrder, setEditingOrder] = useState<Partial<Order> | null>(null);
  // showShipModal 表示订单发货弹窗是否打开。
  const [showShipModal, setShowShipModal] = useState(false);
  // shipOrderId 保存当前待发货订单号。
  const [shipOrderId, setShipOrderId] = useState('');
  // shipLoading 表示发货请求是否正在执行。
  const [shipLoading, setShipLoading] = useState(false);
  // shipResult 保存最近一次发货操作结果。
  // shipResult 保存最近一次发货操作结果。
  const [shipResult, setShipResult] = useState<OrderShipResult | null>(null);
  // syncingOrderId 保存当前正在单笔同步或强制获取的订单号。
  const [syncingOrderId, setSyncingOrderId] = useState<string | null>(null);
  // syncNotice 保存单笔同步或强制获取的结果与等待说明，让用户看到限流而不是静默等待。
  const [syncNotice, setSyncNotice] = useState<string | null>(null);
  // deletingOrderId 保存当前正在删除的订单号。
  const [deletingOrderId, setDeletingOrderId] = useState<string | null>(null);
  // syncGeneration 区分连续发起的批量刷新，旧任务完成后不得覆盖最新一次用户操作的列表和提示。
  const syncGeneration = useRef(0);

  // shipGeneration 绑定当前发货弹窗；关闭、换单或卸载会使旧结果失效，平台动作本身继续完成。
  const shipGeneration = useRef(0);
  // shipBusy 防止同一 Hook 在发货未收束前重复提交；弹窗切换不会取消外部发货。
  const shipBusy = useRef(false);
  // mounted 防止外部发货完成后写入已卸载页面。
  const mounted = useRef(true);
  // deleteGeneration 隔离不同页面和连续删除请求的迟到结果。
  const deleteGeneration = useRef(0);

  useEffect(/* 挂载周期只控制本地展示，清理时不假装撤销已提交的发货。 */ () => {
    mounted.current = true;
    return /* 卸载使弹窗结果失效；在途动作仍由服务端负责完成。 */ () => {
      mounted.current = false;
      shipGeneration.current += 1;
    };
  }, []);

  useEffect(/* 当前查询条件拥有删除后的分页变化，切换时清除旧页面忙碌状态。 */ () => {
    setDeletingOrderId(null);
    return /* 离开本页使旧删除无法递减新页码、刷新旧列表或清理新请求。 */ () => { deleteGeneration.current += 1; };
  }, [accountFilter, filter, page, searchText]);

  // 筛选切换或卸载时使旧同步代次失效；后台任务继续执行，其晚到结果不能刷新旧列表或提示用户。
  useEffect(/* 同步结果归属于当前账号与状态筛选，cleanup 结束该筛选上下文的所有旧代次。 */ () => {
    return () => { syncGeneration.current += 1; }; // 清理当前筛选上下文，不创建新的后台同步任务。
  }, [accountFilter, filter]);

  // handleSync 同步当前筛选条件下的订单并刷新列表。
  const handleSync = useCallback(/* syncAction 执行当前筛选条件下的订单同步。 */ async () => {
		// generation 是本次批量同步的代次；仅仍为最新代次的结果允许更新界面。
		const generation = ++syncGeneration.current;
    try {
      // result 保存订单同步接口返回的结果说明。
      const result = await syncOrders(accountFilter || undefined, filter);
			if (generation !== syncGeneration.current) return;
      await loadOrders();
			if (generation !== syncGeneration.current) return;
      // message 依据结构化失败和恢复统计生成，避免旧后端成功文案掩盖未完成项。
      const message = formatOrderSyncResult(result);
      if (message) alert(message);
    } catch (/* error 表示订单同步请求异常。 */ error: unknown) {
			if (generation !== syncGeneration.current) return;
      console.error('同步订单失败:', error);
      alert(orderErrorMessage(error, '同步失败，请重试'));
    }
  }, [accountFilter, filter, loadOrders]);

  // handleShip 打开发货弹窗并选择待发货订单。
  const handleShip = useCallback(/* shipAction 打开发货弹窗并保存订单号。 */ (orderId: string) => {
    shipGeneration.current += 1;
    setShipOrderId(orderId);
    setShipResult(null);
    setShowShipModal(true);
  }, []);

  // executeShip 执行指定模式的订单发货并更新结果。
  const executeShip = useCallback(/* executeShipAction 执行指定模式的订单发货。 */ async (mode: OrderShipMode) => {
    if (shipBusy.current) return;
    shipBusy.current = true;
    // generation 是提交时弹窗代次，只有同一弹窗可展示结果。
    const generation = shipGeneration.current;
    setShipLoading(true);
    setShipResult(null);
    try {
      // response 保存订单批量发货接口响应。
      const response = await manualShipOrder([shipOrderId], mode);
      if (generation !== shipGeneration.current) return;
      // result 保存当前订单的发货结果行。
      const result = response.results?.[0];
      if (result?.success) {
        setShipResult({ success: true, message: result.message });
        void loadOrders();
      } else {
        setShipResult({ success: false, message: result?.message || '发货失败' });
      }
    } catch (/* error 表示订单发货请求异常。 */ error: unknown) {
      if (generation !== shipGeneration.current) return;
      setShipResult({ success: false, message: orderErrorMessage(error, '请求失败') });
    } finally {
      shipBusy.current = false;
      if (mounted.current) setShipLoading(false);
    }
  }, [loadOrders, shipOrderId]);

  // handleViewDetail 打开指定订单的详情弹窗。
  const handleViewDetail = useCallback(/* detailAction 打开订单详情弹窗。 */ (order: Order) => {
    setSelectedOrder(order);
    setShowDetailModal(true);
  }, []);

  // handleEdit 打开指定订单的编辑弹窗并复制编辑草稿。
  const handleEdit = useCallback(/* editAction 打开订单编辑弹窗。 */ (order: Order) => {
    setEditingOrder({ ...order });
    setShowEditModal(true);
  }, []);

  // updateEditingOrder 使用函数式更新合并订单编辑草稿字段。
  const updateEditingOrder = useCallback(/* draftAction 合并订单编辑草稿字段。 */ (patch: Partial<Order>) => {
    setEditingOrder(/* currentDraft 当前订单编辑草稿。 */ current => current ? { ...current, ...patch } : current);
  }, []);

  // handleSaveEdit 保存当前订单编辑草稿并刷新列表。
  const handleSaveEdit = useCallback(/* saveEditAction 保存订单编辑草稿。 */ async () => {
    if (!editingOrder?.order_id) return;
    try {
      // updateData 保存映射到订单更新接口的字段。
      const updateData: Partial<Order> = {};
      if (editingOrder.status !== undefined) updateData.order_status = editingOrder.status;
      if (editingOrder.buyer_id !== undefined) updateData.buyer_id = editingOrder.buyer_id;
      if (editingOrder.amount !== undefined) updateData.amount = editingOrder.amount;
      if (editingOrder.receiver_name !== undefined) updateData.receiver_name = editingOrder.receiver_name;
      if (editingOrder.receiver_phone !== undefined) updateData.receiver_phone = editingOrder.receiver_phone;
      if (editingOrder.receiver_address !== undefined) updateData.receiver_address = editingOrder.receiver_address;
      if (editingOrder.item_id !== undefined) updateData.item_id = editingOrder.item_id;
      if (editingOrder.quantity !== undefined) updateData.quantity = editingOrder.quantity;
      if (editingOrder.item_title !== undefined) updateData.item_title = editingOrder.item_title;

      await updateOrder(editingOrder.order_id, updateData);
      setShowEditModal(false);
      setEditingOrder(null);
      await loadOrders();
    } catch (/* error 表示订单编辑请求异常。 */ error: unknown) {
      console.error('更新订单失败:', error);
      alert('更新失败，请重试');
    }
  }, [editingOrder, loadOrders]);

  // handleSyncSingle 同步或强制获取指定订单的最新详情并刷新列表。
  // forced 为真表示用户在界面上确认跳过本地账号刷新间隔；请求发出前先提示等待原因，避免按钮转圈期间没有任何说明。
  const handleSyncSingle = useCallback(/* singleSyncAction 执行单笔订单同步或强制获取。 */ async (orderId: string, forced = false) => {
    setSyncingOrderId(orderId);
    setSyncNotice(forced ? '正在强制获取最新订单详情（跳过本地账号刷新间隔）…' : '正在同步订单详情；若该账号刚刷新过，需等待账号刷新间隔…');
    try {
      // result 保存单笔订单同步接口响应，含服务端上报的限流等待时长。
      const result = await syncSingleOrder(orderId, { force: forced });
      setSyncNotice(formatSingleSyncResult(result, forced ? 'force' : 'sync'));
      if (result.success) {
        await loadOrders();
      }
    } catch (/* error 表示单笔订单同步请求异常；平台风控会以 429 错误码返回。 */ error: unknown) {
      console.error('同步订单失败:', error);
      // message 优先使用服务端统一错误说明，保留“平台要求完成风控验证”等可操作信息。
      const message = orderSyncErrorMessage(error, '同步失败，请重试');
      setSyncNotice(message);
    } finally {
      setSyncingOrderId(null);
    }
  }, [loadOrders]);

  // handleForceFetchDetail 在用户明确确认后强制获取订单详情；确认提示说明该操作会跳过本地限流并提高平台风控概率。
  const handleForceFetchDetail = useCallback(/* forceFetchAction 经用户确认后强制获取订单最新详情。 */ async (orderId: string) => {
    if (!confirm('强制获取会跳过本地账号刷新间隔，立即访问闲鱼平台。\n频繁使用可能触发平台风控，是否继续？')) return;
    await handleSyncSingle(orderId, true);
  }, [handleSyncSingle]);

  // handleDelete 删除指定订单并在当前页为空时回退页码。
  const handleDelete = useCallback(/* deleteAction 删除订单并处理分页回退。 */ async (orderId: string) => {
    if (!confirm('确认删除该订单吗？删除后无法恢复。')) return;
    // generation 是当前页面本次删除的唯一代次，旧结果不能作用于新页面。
    const generation = ++deleteGeneration.current;
    setDeletingOrderId(orderId);
    try {
      await deleteOrder(orderId);
      if (generation !== deleteGeneration.current) return;
      if (orders.length === 1 && page > 1) {
        setPage(/* currentPage 当前订单页码。 */ current => Math.max(1, current - 1));
      } else {
        await loadOrders();
      }
    } catch (/* error 表示订单删除请求异常。 */ error: unknown) {
      if (generation !== deleteGeneration.current) return;
      console.error('删除订单失败:', error);
      alert(orderErrorMessage(error, '删除失败，请重试'));
      await loadOrders();
    } finally {
      if (generation === deleteGeneration.current) setDeletingOrderId(null);
    }
  }, [loadOrders, orders.length, page, setPage]);

  // closeDetailModal 关闭订单详情弹窗。
  const closeDetailModal = useCallback(/* closeDetailAction 关闭订单详情弹窗。 */ () => setShowDetailModal(false), []);
  // closeEditModal 关闭订单编辑弹窗。
  const closeEditModal = useCallback(/* closeEditAction 关闭订单编辑弹窗。 */ () => setShowEditModal(false), []);
  // closeShipModal 关闭订单发货弹窗并清理结果。
  const closeShipModal = useCallback(/* closeShipAction 关闭订单发货弹窗。 */ () => {
    shipGeneration.current += 1;
    setShowShipModal(false);
    setShipResult(null);
  }, []);

  return {
    showDetailModal,
    selectedOrder,
    showEditModal,
    editingOrder,
    showShipModal,
    shipOrderId,
    shipLoading,
    shipResult,
    syncingOrderId,
    syncNotice,
    setSyncNotice,
    deletingOrderId,
    handleSync,
    handleShip,
    executeShip,
    handleViewDetail,
    handleEdit,
    handleSaveEdit,
    updateEditingOrder,
    handleSyncSingle,
    handleForceFetchDetail,
    handleDelete,
    closeDetailModal,
    closeEditModal,
    closeShipModal,
  };
};
