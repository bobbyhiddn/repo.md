import os
import pathspec
import logging

# Using a logger
core_logger = logging.getLogger(__name__)

def is_readable(file_path):
    """Check if a file is readable as text."""
    try:
        with open(file_path, 'r', encoding='utf-8') as file:
            file.read(1024)
            return True
    except (UnicodeDecodeError, IOError):
        return False


def read_directory_to_markdown(path, repo_root_path=None, current_markdown_path_prefix="", ignore_git=True, _initial_call=True, _gitignore_patterns_cache=None):
    """Recursively read a directory and convert to Markdown."""
    contents = ""
    if repo_root_path is None:
        repo_root_path = path
    if _initial_call and _gitignore_patterns_cache is None:
        _gitignore_patterns_cache = {}
    if ignore_git and repo_root_path not in _gitignore_patterns_cache:
        gi = os.path.join(repo_root_path, '.gitignore')
        if os.path.exists(gi):
            try:
                with open(gi, 'r', encoding='utf-8') as f:
                    patterns = [p.strip() for p in f if p.strip()]
                spec = pathspec.PathSpec.from_lines('gitwildmatch', patterns)
                core_logger.info(f"Loaded .gitignore from {gi}")
            except Exception as e:
                core_logger.error(f"Error reading .gitignore: {e}")
                spec = pathspec.PathSpec.from_lines('gitwildmatch', [])
        else:
            spec = pathspec.PathSpec.from_lines('gitwildmatch', [])
        _gitignore_patterns_cache[repo_root_path] = spec
    active = _gitignore_patterns_cache.get(repo_root_path) if ignore_git else None
    try:
        items = sorted(os.listdir(path))
    except Exception as e:
        core_logger.error(f"Cannot list {path}: {e}")
        return f"Error listing {path}\n"
    for item in items:
        full = os.path.join(path, item)
        rel = os.path.relpath(full, repo_root_path).replace(os.sep, '/')
        if ignore_git and item == '.git': continue
        key = rel + ('/' if os.path.isdir(full) else '')
        if active and active.match_file(key): continue
        disp = f"{current_markdown_path_prefix}/{item}"
        if os.path.isdir(full):
            contents += f"\n## {disp}/\n\n"
            contents += read_directory_to_markdown(full, repo_root_path, disp, ignore_git, False, _gitignore_patterns_cache)
        else:
            line = f"{disp}: "
            if is_readable(full):
                try:
                    with open(full, 'r', encoding='utf-8', errors='replace') as f:
                        txt = f.read()
                    line += f"\n```\n{txt}\n```\n"
                except Exception as e:
                    core_logger.error(f"Read error {full}: {e}")
                    line += "[error reading]\n"
            else:
                line += "[non-readable]\n"
            contents += f"\n{line}\n"
    return contents
