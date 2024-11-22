# Diffs:
v1 Pod another/nginx-pod:

v1 Pod default/nginx-pod:
--- default/nginx-pod : existing
+++ default/nginx-pod : updated
@@ -6,7 +6,8 @@
 spec:
   containers:
   - image: nginx:1.7.9
     name: nginx
     ports:
+    - containerPort: 8080
     - containerPort: 80
 
v1 Pod nonexist/nginx-pod:

# Compliance messages:
NonCompliant; notification - pods [nginx-pod] found as specified in namespace another; violation - pods [nginx-pod] found but not as specified in namespace default; violation - pods [nginx-pod] not found in namespace nonexist
