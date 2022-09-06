# Karmada Proxy 容量测试

## 目标

对proxy容量进行摸底，发现性能问题以指导组件优化。

## 测量指标

- search组件的CPU和内存消耗

- search 缓存重建时间

- 客户端失败率、请求延迟

- pprof

## 测试方法

### 1. 准备测试环境

- clusters: 100

- pods: 200w

### 2. 拉取测试分支

```bash
git clone -b benchmark https://github.com/ikaven1024/karmada.git
```

### 3. 替换search组件

从分支中重新编译karmada search组件，部署到测试环境上。并且开启pprof和日志。

```
--enable-pprof=true
--v=3
```

>search组件修改点：
>
>- 关闭search informer

### 4. 测试对照组

直连其中一个member集群（例如member1），执行测试：

```bash
export KUBECONFIG=/path/to/member/kubeconfig
hack/run-benchmark.sh --context=member1
```

>默认测试容器是在default命名空间下，如果不是，请使用`-test-namespace`参数指定。
>
>脚本使用方法详见：https://github.com/ikaven1024/karmada/blob/benchmark/hack/run-benchmark.sh#L7-L38

测试中将依此进行3组用例的执行，每组运行5分钟：

- list pods
- get pod
- update pod

执行后脚本会输出测试结果，记录失败率以及延时统计，例如：

```
E0909 16:42:27.247791   57094 search_test.go:124] error trying to reach service: dial tcp 10.86.130.17:8044: connect: operation timed out   <==== 接口失败信息
I0909 16:42:27.313389   57094 search_test.go:131] Update pod [errors]: 40/343           <=== 接口失败统计
• [SLOW TEST] [29.960 seconds]
Search bench test update pod [measurement]
/Users/didi/OpenSouorce/karmada/test/benchmark/search_test.go:86

  Begin Report Entries >>
    Update pod - /Users/didi/OpenSouorce/karmada/test/benchmark/search_test.go:117 @ 09/09/22 16:41:57.369
      Update pod
      Name                  | N   | Min     | Median  | Mean    | StdDev | Max          <===== 接口延时统计
      ============================================================================
      Update pod [duration] | 294 | 204.3ms | 228.1ms | 236.6ms | 38.5ms | 490.4ms
  << End Report Entries
------------------------------
```



### 5. 测试实验组

从 karmada apiserver kubeconfig 文件复制出一份作为proxy的kubeconfig，在`server`地址后追加`/apis/search.karmada.io/v1alpha1/proxying/karmada/proxy`。如：

```yaml
apiVersion: v1
clusters:
- cluster:
    server: https://127.0.0.1:5443/apis/search.karmada.io/v1alpha1/proxying/karmada/proxy
...
```

执行测试脚本，并记录数据（如上）。

```bash
export KUBECONFIG=/path/to/proxy/kubeconfig
hack/run-benchmark.sh
```

### 6. 缓存重建时间

查看karmada search日志，查找关键字`Add cache for` 和`cacher (*unstructured.Unstructured): initialized`，分别表示缓存重建的开始时间和完成时间。记录第一个开始时间和最后的结束时间，其时间差就是总共的重建时间。

```
I0908 19:49:35.691264   11667 multi_cluster_cache.go:84] Add cache for cluster member1               <==== member1开始
I0908 19:49:35.691428   11667 cluster_cache.go:58] Add cache for member1 /v1, Resource=pods
I0908 19:49:35.691634   11667 multi_cluster_cache.go:84] Add cache for cluster member2               <==== member2开始
I0908 19:49:35.691682   11667 cluster_cache.go:58] Add cache for member2 /v1, Resource=pods
I0908 19:49:35.691796   11667 reflector.go:255] Listing and watching /v1, Kind=Pod from storage/cacher.go:/pods
I0908 19:49:35.699618   11667 get.go:260] "Starting watch" path="/apis/search.karmada.io/v1alpha1/resourceregistries" resourceVersion="1142894" labels="" fields="" timeout="6m32s
I0908 19:49:36.029936   11667 cacher.go:410] cacher (*unstructured.Unstructured): initialized        <==== member1结束
I0908 19:49:36.029964   11667 watch_cache.go:566] Replace watchCache (rev: 3811226) 
I0908 19:49:36.052463   11667 cacher.go:410] cacher (*unstructured.Unstructured): initialized        <==== member2结束
I0908 19:49:36.052481   11667 watch_cache.go:566] Replace watchCache (rev: 3507668) 
```

### 7. pprof

先查询karmada search 组件的IP，下载heap数据：

```
curl http://IP:6060/debug/pprof/heap > heap.out
```

查看：

```
go tool pprof -http=:8081 heap.out
```

## 记录

| 指标     | 测试组（100 Clusters, 200W Pods） | 对比结果 |
|--------|------------------------------|------|
| CPU    |                              |      |
| 内存     |                              |      |
| 缓存重建时间 |                              |      |

**list pods**

- 对照组（单集群 2W pods）

```
      List pods [errors]: 0/24350
      Name                 | N     | Min   | Median | Mean   | StdDev | Max    
      =========================================================================
      List pods [duration] | 24340 | 1.7ms | 8.7ms  | 12.3ms | 24.6ms | 707.3ms
```

- 测试组（100 Clusters, 200W Pods）

```
      
```

**get pod**


- 对照组（单集群 2W pods）

```
      
```

- 测试组（100 Clusters, 200W Pods）

```
      
```

**update pod**


- 对照组（单集群 2W pods）

```
      
```

- 测试组（100 Clusters, 200W Pods）

```
      
```

**pprof:**



**benchmark日志：**



**karmada-search日志：**



## 结果分析

