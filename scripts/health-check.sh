#!/bin/bash
# Script para verificar el estado de salud de los microservicios desplegados
# Uso: ./health-check.sh [namespace]

NAMESPACE=${1:-prod}
TIMEOUT=60
RETRIES=3

echo "=========================================="
echo "HEALTH CHECK - MICROSERVICIOS"
echo "=========================================="
echo "Namespace: $NAMESPACE"
echo "Fecha: $(date)"
echo ""

# Verificar que kubectl está instalado
if ! command -v kubectl &> /dev/null; then
    echo "❌ Error: kubectl no está instalado"
    exit 1
fi

# Verificar conexión al cluster
if ! kubectl cluster-info &>/dev/null; then
    echo "❌ Error: No se puede conectar al cluster Kubernetes"
    echo "Verifica tu configuración de kubectl"
    exit 1
fi

echo "✅ Conectado al cluster"
echo ""

# ==========================================
# VERIFICAR PODS
# ==========================================
echo "=========================================="
echo "1. ESTADO DE PODS"
echo "=========================================="
echo ""

kubectl get pods -n "$NAMESPACE" -o wide

echo ""
echo "Verificando pods en estado Running..."
NOT_RUNNING=$(kubectl get pods -n "$NAMESPACE" --field-selector=status.phase!=Running -o name 2>/dev/null)

if [ -n "$NOT_RUNNING" ]; then
    echo "⚠️  Pods que no están en estado Running:"
    echo "$NOT_RUNNING"
    echo ""
    echo "Detalles de pods con problemas:"
    for pod in $NOT_RUNNING; do
        echo "--- $pod ---"
        kubectl describe "$pod" -n "$NAMESPACE" | grep -A 5 "Events:"
        echo ""
    done
else
    echo "✅ Todos los pods están en estado Running"
fi

echo ""

# ==========================================
# VERIFICAR DEPLOYMENTS
# ==========================================
echo "=========================================="
echo "2. ESTADO DE DEPLOYMENTS"
echo "=========================================="
echo ""

kubectl get deployments -n "$NAMESPACE"

echo ""
echo "Verificando disponibilidad..."

deployments=$(kubectl get deployments -n "$NAMESPACE" -o name)
ALL_AVAILABLE=true

for deployment in $deployments; do
    name=$(echo "$deployment" | cut -d/ -f2)
    
    desired=$(kubectl get deployment "$name" -n "$NAMESPACE" -o jsonpath='{.spec.replicas}')
    available=$(kubectl get deployment "$name" -n "$NAMESPACE" -o jsonpath='{.status.availableReplicas}')
    
    desired=${desired:-0}
    available=${available:-0}
    
    if [ "$desired" -eq "$available" ]; then
        echo "✅ $name: $available/$desired réplicas disponibles"
    else
        echo "⚠️  $name: $available/$desired réplicas disponibles"
        ALL_AVAILABLE=false
    fi
done

echo ""

# ==========================================
# VERIFICAR SERVICIOS
# ==========================================
echo "=========================================="
echo "3. ESTADO DE SERVICIOS"
echo "=========================================="
echo ""

kubectl get services -n "$NAMESPACE"

echo ""

# ==========================================
# VERIFICAR ENDPOINTS (Circuit Breaker)
# ==========================================
echo "=========================================="
echo "4. VERIFICACIÓN DE ENDPOINTS"
echo "=========================================="
echo ""

# Intentar port-forward para health check
for service in vote result; do
    echo "Verificando endpoint de $service..."
    
    # Verificar si el servicio existe
    if kubectl get service "$service" -n "$NAMESPACE" &>/dev/null; then
        echo "✅ Servicio $service existe"
        
        # Mostrar endpoints
        endpoints=$(kubectl get endpoints "$service" -n "$NAMESPACE" -o jsonpath='{.subsets[*].addresses[*].ip}' 2>/dev/null)
        if [ -n "$endpoints" ]; then
            echo "   Endpoints: $endpoints"
        else
            echo "   ⚠️  No hay endpoints activos"
        fi
    else
        echo "❌ Servicio $service no encontrado"
    fi
    echo ""
done

# ==========================================
# VERIFICAR KAFKA CONSUMER GROUPS
# ==========================================
echo "=========================================="
echo "5. VERIFICACIÓN DE KAFKA (Competing Consumer)"
echo "=========================================="
echo ""

# Buscar un pod de Kafka
kafka_pod=$(kubectl get pods -n "$NAMESPACE" -l app=kafka -o name 2>/dev/null | head -1)

if [ -n "$kafka_pod" ]; then
    echo "Kafka pod encontrado: $kafka_pod"
    
    # Verificar consumer groups
    echo ""
    echo "Consumer Groups activos:"
    kubectl exec -n "$NAMESPACE" "$kafka_pod" -- \
        bash -c '/opt/kafka/bin/kafka-consumer-groups.sh --bootstrap-server localhost:9092 --list' 2>/dev/null || \
        echo "No se pudieron listar consumer groups"
    
    echo ""
    echo "Detalle del consumer group 'vote-workers':"
    kubectl exec -n "$NAMESPACE" "$kafka_pod" -- \
        bash -c '/opt/kafka/bin/kafka-consumer-groups.sh --bootstrap-server localhost:9092 --describe --group vote-workers' 2>/dev/null || \
        echo "Consumer group 'vote-workers' no encontrado o error de conexión"
else
    echo "⚠️  No se encontró pod de Kafka en el namespace $NAMESPACE"
fi

echo ""

# ==========================================
# VERIFICAR LOGS RECIENTES (Circuit Breaker)
# ==========================================
echo "=========================================="
echo "6. LOGS RECIENTES DE WORKERS (Circuit Breaker)"
echo "=========================================="
echo ""

worker_pods=$(kubectl get pods -n "$NAMESPACE" -l app=worker -o name 2>/dev/null)

if [ -n "$worker_pods" ]; then
    for pod in $worker_pods; do
        pod_name=$(echo "$pod" | cut -d/ -f2)
        echo "--- Últimos logs de $pod_name ---"
        kubectl logs "$pod" -n "$NAMESPACE" --tail=5 2>/dev/null || echo "No se pudieron obtener logs"
        echo ""
        
        # Buscar señales de circuit breaker
        cb_logs=$(kubectl logs "$pod" -n "$NAMESPACE" --tail=20 2>/dev/null | grep -i "circuit\|retrying\|failed" || true)
        if [ -n "$cb_logs" ]; then
            echo "   🔄 Eventos de Circuit Breaker/Retry detectados:"
            echo "$cb_logs" | head -3
        fi
        echo ""
    done
else
    echo "⚠️  No se encontraron pods de worker"
fi

# ==========================================
# RESUMEN FINAL
# ==========================================
echo "=========================================="
echo "RESUMEN DE HEALTH CHECK"
echo "=========================================="
echo ""

if [ "$ALL_AVAILABLE" = true ] && [ -z "$NOT_RUNNING" ]; then
    echo "✅ TODOS LOS SERVICIOS ESTÁN SALUDABLES"
    exit 0
else
    echo "⚠️  ALGUNOS SERVICIOS TIENEN PROBLEMAS"
    echo "Revisa los detalles arriba"
    exit 1
fi
