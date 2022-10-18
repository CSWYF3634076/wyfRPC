package wyfrpc

import (
	"go/ast"
	"log"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
)

// example 使用反射将结构体映射为服务的例子
func example() {
	var wg sync.WaitGroup
	typ := reflect.TypeOf(&wg)
	for i := 0; i < typ.NumMethod(); i++ {
		method := typ.Method(i)
		argv := make([]string, 0, method.Type.NumIn())
		returns := make([]string, 0, method.Type.NumOut())
		// j 从 1 开始，第 0 个入参是 wg 自己。
		for j := 1; j < method.Type.NumIn(); j++ {
			argv = append(argv, method.Type.In(j).Name())
		}
		for j := 0; j < method.Type.NumOut(); j++ {
			returns = append(returns, method.Type.Out(j).Name())
		}
		log.Printf("func (w *%s) %s(%s) %s",
			typ.Elem().Name(),
			method.Name,
			strings.Join(argv, ","),
			strings.Join(returns, ","))
	}
}

// 通过反射实现service
// 每一个 methodType 实例包含了一个方法的完整信息。包括
type methodType struct {
	method    reflect.Method // 方法本身
	ArgType   reflect.Type   // 第一个参数的类型
	ReplyType reflect.Type   //第二个参数的类型
	numCalls  uint64         // 后续统计方法调用次数时会用到
}

func (m *methodType) NumCalls() uint64 {
	return atomic.LoadUint64(&m.numCalls)
}

//  2 个方法 newArgv 和 newReplyv，用于创建对应类型的实例。
// newArgv 方法有一个小细节，指针类型和值类型创建实例的方式有细微区别。
// 主要将 reflect.Type 转为 reflect.Value
func (m *methodType) newArgv() reflect.Value {
	var argv reflect.Value
	// arg may be a pointer type, or a value type
	if m.ArgType.Kind() == reflect.Pointer {
		argv = reflect.New(m.ArgType.Elem())
	} else {
		argv = reflect.New(m.ArgType).Elem()
	}
	return argv
}

func (m *methodType) newReplyv() reflect.Value {
	// reply must be a pointer type
	replyv := reflect.New(m.ReplyType.Elem())
	switch m.ReplyType.Elem().Kind() {
	case reflect.Map:
		replyv.Elem().Set(reflect.MakeMap(m.ReplyType.Elem()))
	case reflect.Slice:
		replyv.Elem().Set(reflect.MakeSlice(m.ReplyType.Elem(), 0, 0))
	}
	return replyv
}

/*
service 的定义也是非常简洁的，
name 即映射的结构体的名称，比如 T，比如 WaitGroup；
typ 是结构体的类型；
rcvr 即结构体的实例本身，保留 rcvr 是因为在调用时需要 rcvr 作为第 0 个参数；
method 是 map 类型，存储映射的结构体的所有符合条件的方法。
*/
type service struct {
	typName string
	typ     reflect.Type
	rcvr    reflect.Value
	method  map[string]*methodType
}

//  newService，入参是任意需要映射为服务的结构体实例。
func newService(rcvr any) *service {
	s := new(service)
	s.rcvr = reflect.ValueOf(rcvr)
	s.typ = reflect.TypeOf(rcvr)
	s.typName = reflect.Indirect(s.rcvr).Type().Name()
	if !ast.IsExported(s.typName) {
		log.Fatalf("rpc server: %s is not a valid service name", s.typName)
	}
	s.registerMethods()
	return s
}

/*
registerMethods 过滤出了符合条件的方法：
两个导出或内置类型的入参
（反射时为 3 个，第 0 个是自身，类似于 python 的 self，java 中的 this）
返回值有且只有 1 个，类型为 error
*/
func (s *service) registerMethods() {
	s.method = make(map[string]*methodType)
	for i := 0; i < s.typ.NumMethod(); i++ {
		// 要从反射的type里遍历Method
		method := s.typ.Method(i)
		mType := method.Type
		if mType.NumIn() != 3 || mType.NumOut() != 1 {
			continue
		}
		if mType.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
			continue
		}
		// 方法的 参数0 是方法名 ，索引 1 是参数，索引 2 是返回值 ；和具体要注册的函数有关
		// 参考main.go中的 Foo的Sum函数
		argType, replyType := mType.In(1), mType.In(2)
		if !isExportedOrBuiltinType(argType) || !isExportedOrBuiltinType(replyType) {
			continue
		}
		s.method[method.Name] = &methodType{
			method:    method,
			ArgType:   argType,
			ReplyType: replyType,
		}
		log.Printf("rpc server: register %s.%s\n", s.typName, method.Name)

	}
}

func isExportedOrBuiltinType(t reflect.Type) bool {
	return ast.IsExported(t.Name()) || t.PkgPath() == ""
}

// 需要实现 call 方法，即能够通过反射值调用方法
func (s *service) call(m *methodType, argv, replyv reflect.Value) error {
	atomic.AddUint64(&m.numCalls, 1)
	f := m.method.Func
	returnValues := f.Call([]reflect.Value{s.rcvr, argv, replyv})
	if errInter := returnValues[0].Interface(); errInter != nil {
		return errInter.(error)
	}
	return nil
}
