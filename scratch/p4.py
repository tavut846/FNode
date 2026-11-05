p = 'FNode-script/FNode.sh'
s = open(p, encoding='utf-8', newline='').read()
old = '(true|false)/${val}/" "$f"'
new = '(true|false)/' + chr(92) + '1${val}/" "$f"'
assert s.count(old) == 1
open(p, 'w', encoding='utf-8', newline='').write(s.replace(old, new))
