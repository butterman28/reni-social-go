import os
import re

def parse_go_functions(lines):
    """Parses all Go function signatures in a list of lines."""
    functions = []
    i = 0
    while i < len(lines):
        line = lines[i]
        # Match standard Go function declarations (handles methods with receivers too)
        match = re.match(r'^func\s+(?:\([^)]+\)\s+)?([A-Za-z0-9_]+)\s*\(', line)
        if match:
            func_name = match.group(1)
            sig_lines = [line]
            start_i = i
            
            # Read until we hit the opening brace '{' of the function body
            while '{' not in line and i + 1 < len(lines):
                i += 1
                line = lines[i]
                sig_lines.append(line)
                
            sig = "".join(sig_lines).replace("\n", " ").replace("\r", "")
            
            # Skip receiver parenthesis if it exists
            if sig.startswith("func ("):
                depth = 0
                open_paren = -1
                for idx, char in enumerate(sig):
                    if char == '(':
                        depth += 1
                        if depth == 1 and open_paren == -1:
                            open_paren = idx
                    elif char == ')':
                        depth -= 1
                        if depth == 0:
                            open_paren = sig.find('(', idx)
                            break
            else:
                open_paren = sig.find('(')
                
            if open_paren != -1:
                depth = 0
                remaining_sig = ""
                for idx in range(open_paren, len(sig)):
                    if sig[idx] == '(':
                        depth += 1
                    elif sig[idx] == ')':
                        depth -= 1
                        if depth == 0:
                            args_str = sig[open_paren+1:idx]
                            remaining_sig = sig[idx+1:]
                            break
                
                remaining_sig = remaining_sig.strip()
                if remaining_sig.startswith('{'):
                    returns_str = ""
                elif remaining_sig.startswith('('):
                    close_paren = remaining_sig.find(')')
                    returns_str = remaining_sig[1:close_paren]
                else:
                    brace_idx = remaining_sig.find('{')
                    if brace_idx != -1:
                        returns_str = remaining_sig[:brace_idx].strip()
                    else:
                        returns_str = remaining_sig.strip()
                
                functions.append({
                    "name": func_name,
                    "args": args_str,
                    "returns": returns_str,
                    "start_idx": start_i,
                    "end_idx": i
                })
        i += 1
    return functions

def get_swagger_annotations(func_name, args_str, returns_str, pkg_name, handlers_map):
    """Generates Swagger tags based on function signature and route data."""
    key = (pkg_name, func_name)
    routes = handlers_map.get(key, [])
    is_handler = len(routes) > 0

    # 🛑 GUARD: Middleware functions or unmapped middleware package functions should not get endpoint annotations
    if pkg_name == "middleware" and not is_handler:
        return []

    # 🛑 GUARD: Skip functions returning fiber.Handler or raw func
    is_middleware = False
    if returns_str.strip():
        if any(bad_type in returns_str for bad_type in ["fiber.Handler", "func(", "http.Handler", "gin.HandlerFunc"]):
            is_middleware = True

    if is_middleware and not is_handler:
        return []

    annotations = []
    annotations.append(f"// {func_name} godoc")
    annotations.append(f"// @Summary {func_name}")
    annotations.append(f"// @Description Auto-generated documentation for {func_name}")
    annotations.append(f"// @Tags {pkg_name}")
    annotations.append(f"// @Produce json")
    
    method = ""
    if is_handler:
        method, full_path = routes[0]
        if "/api/in" in full_path:
            annotations.append(f"// @Security BearerAuth")
            annotations.append(f"// @Security SessionId")
            
        path_params = re.findall(r":(\w+)", full_path)
        for p in path_params:
            annotations.append(f'// @Param {p} path string true "{p}"')
            
        if "/posts" in full_path and method == "GET" and not full_path.endswith("/results"):
            annotations.append(f'// @Param workspace_id query string true "Workspace ID"')
        if full_path.endswith("/results") and method == "GET":
            annotations.append(f'// @Param attempt_id query string true "Publish attempt ID"')
            
        for r_method, r_path in routes:
            r_swagger_path = re.sub(r":(\w+)", r"{\1}", r_path)
            annotations.append(f"// @Router {r_swagger_path} [{r_method.lower()}]")

    # Parse Arguments to @Param
    if args_str.strip():
        args = [a.strip() for a in args_str.split(',')]
        for arg in args:
            if not arg: continue
            parts = arg.split()
            if len(parts) >= 2:
                arg_name = parts[0]
                arg_type = " ".join(parts[1:])
                
                # Ignore non-serializable parameters
                if any(x in arg_type for x in ["fiber.Ctx", "Context", "fiber.Handler", "func("]):
                    continue
                
                swag_type = "string"
                if "int" in arg_type: swag_type = "integer"
                elif "bool" in arg_type: swag_type = "boolean"
                elif "float" in arg_type: swag_type = "number"
                elif "*" in arg_type or "struct" in arg_type or "map" in arg_type: swag_type = "object"
                elif "[]" in arg_type: swag_type = "array"
                
                if swag_type in ("object", "array"):
                    clean_type = arg_type.replace("*", "").replace("[]", "")
                    if len(clean_type) > 0 and clean_type[0].isupper():
                        annotations.append(f'// @Param {arg_name} body {clean_type} true "{arg_name}"')
                    else:
                        annotations.append(f'// @Param {arg_name} body {swag_type} true "{arg_name}"')
                else:
                    annotations.append(f'// @Param {arg_name} body {swag_type} true "{arg_name}"')

    if not is_middleware:
        success_type = "object"
        success_model = "map[string]any"
        
        if returns_str.strip():
            returns = [r.strip() for r in returns_str.split(',')]
            has_error = any("error" in r for r in returns)
            
            for r in returns:
                if r == "error": continue
                
                parts = r.split()
                if parts:
                    t = parts[-1]
                    # Filter out function types from success_model
                    if any(bad in t for bad in ["fiber.Handler", "func", "fiber.Ctx"]):
                        continue

                    if t.startswith("*"):
                        success_model = t[1:]
                        success_type = "object"
                    elif t.startswith("[]"):
                        success_model = t[2:]
                        success_type = "array"
                    elif t in ("int", "int32", "int64"):
                        success_type = "integer"
                        success_model = t
                    elif t == "bool":
                        success_type = "boolean"
                        success_model = t
                    elif t == "string":
                        success_type = "string"
                        success_model = t
                    else:
                        success_model = t
                        
            success_code = "200"
            if is_handler:
                if method == "POST":
                    success_code = "201"
                elif method == "DELETE":
                    success_code = "204"
                    
            annotations.append(f'// @Success {success_code} {{{success_type}}} {success_model} "Successful operation"')
            
            if has_error:
                annotations.append(f'// @Failure 400 {{object}} map[string]string "Bad Request"')
                annotations.append(f'// @Failure 401 {{object}} map[string]string "Unauthorized"')
                annotations.append(f'// @Failure 403 {{object}} map[string]string "Forbidden"')
                annotations.append(f'// @Failure 404 {{object}} map[string]string "Not Found"')
                annotations.append(f'// @Failure 500 {{object}} map[string]string "Internal Server Error"')
            else:
                annotations.append(f'// @Failure 500 {{object}} map[string]string "Internal Server Error"')
        else:
            success_code = "200"
            if is_handler:
                if method == "POST":
                    success_code = "201"
                elif method == "DELETE":
                    success_code = "204"
            annotations.append(f'// @Success {success_code} {{{success_type}}} {success_model} "Successful operation"')
            annotations.append(f'// @Failure 500 {{object}} map[string]string "Internal Server Error"')
            
    return annotations

def main():
    routes_file = "internal/routes/routes.go"
    if not os.path.exists(routes_file):
        print(f"❌ Could not find {routes_file}")
        return

    with open(routes_file, "r", encoding="utf-8") as f:
        routes_content = f.read()

    groups = {"app": ""}
    group_pattern = re.compile(r'(\w+)\s*:=\s*(\w+)\.Group\(\s*"([^"]+)"')
    for match in group_pattern.finditer(routes_content):
        var_name, parent_var, prefix = match.groups()
        parent_path = groups.get(parent_var, "")
        groups[var_name] = parent_path + prefix

    route_pattern = re.compile(
        r'(\w+)\.(Get|Post|Put|Delete|Patch)\(\s*"([^"]+)"\s*,\s*(\w+)\.(\w+)'
    )
    handlers_map = {}

    for match in route_pattern.finditer(routes_content):
        group_var, method, sub_path, pkg, func_name = match.groups()
        base_path = groups.get(group_var, "")
        full_path = base_path + sub_path
        method = method.upper()

        key = (pkg, func_name)
        if key not in handlers_map:
            handlers_map[key] = []
        handlers_map[key].append((method, full_path))

    go_files = []
    for root, dirs, files in os.walk("internal"):
        for file in files:
            if file.endswith(".go") and not file.endswith("_test.go"):
                go_files.append(os.path.join(root, file))

    modified_count = 0

    for filepath in go_files:
        with open(filepath, "r", encoding="utf-8") as f:
            lines = f.readlines()

        pkg_name = ""
        for line in lines:
            m = re.match(r"^package\s+(\w+)", line)
            if m:
                pkg_name = m.group(1)
                break

        if not pkg_name:
            continue

        functions = parse_go_functions(lines)
        
        new_lines = []
        changed = False
        i = 0
        
        for func in functions:
            while i < func["start_idx"]:
                new_lines.append(lines[i])
                i += 1
                
            already_annotated = False
            j = func["start_idx"] - 1
            while j >= 0:
                line_content = lines[j].strip()
                if not line_content:
                    j -= 1
                    continue
                if line_content.startswith("//"):
                    if "@Router" in line_content or "@Summary" in line_content:
                        already_annotated = True
                    j -= 1
                else:
                    break
                    
            if not already_annotated:
                annotations = get_swagger_annotations(
                    func["name"], func["args"], func["returns"], pkg_name, handlers_map
                )
                if annotations:
                    new_lines.extend([a + "\n" for a in annotations])
                    changed = True
                
            while i <= func["end_idx"]:
                new_lines.append(lines[i])
                i += 1
                
        while i < len(lines):
            new_lines.append(lines[i])
            i += 1

        if changed:
            with open(filepath, "w", encoding="utf-8") as f:
                f.writelines(new_lines)
            modified_count += 1
            print(f"✅ Annotated: {filepath}")

    print(f"\n🎉 Done! Injected Swagger comments into {modified_count} files.")

if __name__ == "__main__":
    main()