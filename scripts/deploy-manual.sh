#!/bin/bash
# Script para despliegue manual de microservicios a AKS
# Útil para pruebas locales o rollback de emergencia

set -e

NAMESPACE="${1:-staging}"
TAG="${2:-latest}"
ACR="${ACR_LOGIN_SERVER:-<tu-registry>.azurecr.io}"

echo "=========================================="
echo "DESPLIEGUE MANUAL A AKS"
echo "=========================================="
echo "Namespace: $NAMESPACE"
echo "Tag: $TAG"
echo "ACR: $ACR"
echo ""

# Verificar dependencias
for cmd in kubectl helm docker; do
    if ! command -v "$cmd" &> /dev/null; then
        echo "❌ Error: $cmd no está instalado"
        exit 1
    fi
done

echo "✅ Todas las dependencias están instaladas"
echo ""

# Verificar conexión a AKS
echo "Verificando conexión al cluster AKS..."
kubectl cluster-info || {
    echo "❌ No se puede conectar al cluster"
    echo "Asegúrate de tener configurado kubectl con el contexto correcto"
    exit 1
}

# Verificar namespace
if ! kubectl get namespace "$NAMESPACE" &>/dev/null; then
    echo "Creando namespace $NAMESPACE..."
    kubectl create namespace "$NAMESPACE"
fi

# ==========================================
# DESPLIEGUE DE INFRAESTRUCTURA
# ==========================================
echo ""
echo "=========================================="
echo "1. DESPLIEGUE DE INFRAESTRUCTURA"
echo "=========================================="
echo ""

if [ -d "infrastructure" ]; then
    echo "Aplicando manifests de infraestructura..."
    kubectl apply -f infrastructure/templates/ -n "$NAMESPACE" || {
        echo "⚠️  Algunos recursos de infraestructura no se aplicaron (puede que ya existan)"
    }
else
    echo "⚠️  Directorio infrastructure/ no encontrado, saltando..."
fi

# Esperar a que Kafka y PostgreSQL estén listos
echo ""
echo "Esperando a que PostgreSQL esté listo..."
kubectl wait --for=condition=ready pod -l app=postgresql -n "$NAMESPACE" --timeout=120s || {
    echo "⚠️  Timeout esperando PostgreSQL, continuando..."
}

echo "Esperando a que Kafka esté listo..."
kubectl wait --for=condition=ready pod -l app=kafka -n "$NAMESPACE" --timeout=120s || {
    echo "⚠️  Timeout esperando Kafka, continuando..."
}

# ==========================================
# DESPLIEGUE DE MICROSERVICIOS
# ==========================================
echo ""
echo "=========================================="
echo "2. DESPLIEGUE DE MICROSERVICIOS"
echo "=========================================="
echo ""

for service in vote worker result; do
    echo ""
    echo "Desplegando $service..."
    
    IMAGE="${ACR}/${service}:${TAG}"
    
    if [ -d "${service}/chart" ]; then
        # Usar Helm
        replicas=3
        if [ "$NAMESPACE" == "prod" ]; then
            replicas=5
        fi
        
        helm upgrade --install "$service" "${service}/chart" \
            --namespace "$NAMESPACE" \
            --set "image=${IMAGE}" \
            --set "replicaCount=${replicas}" \
            --wait \
            --timeout 5m \
            --atomic
    else
        # Fallback con kubectl
        kubectl set image "deployment/${service}" "${service}=${IMAGE}" -n "$NAMESPACE" || {
            echo "⚠️  Deployment ${service} no encontrado, intentando crear..."
            # Aquí podrías aplicar un manifest genérico si existe
        }
    fi
    
    echo "✅ $service desplegado"
done

# ==========================================
# VERIFICACIÓN
# ==========================================
echo ""
echo "=========================================="
echo "3. VERIFICACIÓN DEL DESPLIEGUE"
echo "=========================================="
echo ""

echo "Estado de pods:"
kubectl get pods -n "$NAMESPACE"

echo ""
echo "Esperando a que todos los pods estén listos..."
for service in vote worker result; do
    kubectl wait --for=condition=ready pod -l "app=${service}" -n "$NAMESPACE" --timeout=120s || {
        echo "⚠️  Timeout esperando ${service}"
    }
done

echo ""
echo "=========================================="
echo "✅ DESPLIEGUE COMPLETADO"
echo "=========================================="
echo ""
echo "Servicios disponibles:"
kubectl get services -n "$NAMESPACE"
echo ""
echo "Para ver logs:"
echo "  kubectl logs -f deployment/<servicio> -n $NAMESPACE"
echo ""
echo "Para port-forward:"
echo "  kubectl port-forward svc/vote 8080:80 -n $NAMESPACE"
